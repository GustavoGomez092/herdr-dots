package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/platform/platformtest"
)

// widgetSet is a test-only kind at version 3 with two JSON migrations: v1
// renames "name" to "title", v2 adds "tags".
func widgetSet(lock func(string) string) Set {
	step := func(to int, fn func(map[string]any)) func([]byte) ([]byte, error) {
		return func(b []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			fn(m)
			m["version"] = to
			return json.Marshal(m)
		}
	}
	return Set{
		Kinds: []Kind{{Name: "widget", Root: StateRoot, Globs: []string{"widgets/*.json"}, Format: JSON, Current: 3, Lock: lock}},
		Migrations: []Migration{
			{Kind: "widget", From: 1, Apply: step(2, func(m map[string]any) { m["title"] = m["name"]; delete(m, "name") })},
			{Kind: "widget", From: 2, Apply: step(3, func(m map[string]any) { m["tags"] = []string{} })},
		},
	}
}

func noLock(string) string { return "" }

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProbeReadsVersionsAndDefaultsToOne(t *testing.T) {
	cases := []struct {
		data string
		f    Format
		want int
		bad  bool
	}{
		{`{"next":3,"cards":[]}`, JSON, 1, false},
		{`{"version":2,"cards":[]}`, JSON, 2, false},
		{`{"w1:p1":{"pane_id":"w1:p1"}}`, JSON, 1, false}, // map-shaped file
		{`{"version":{"pane_id":"x"}}`, JSON, 1, false},   // a pane named "version"
		{`null`, JSON, 1, false},                          // an empty store
		{`{"version":0}`, JSON, 0, true},                  // never written by herdr-dots
		{`[1,2]`, JSON, 0, true},                          // not an object
		{``, JSON, 0, true},                               // truncated
		{"name = \"Security\"\n", TOML, 1, false},         // a v1 dot
		{"version = 4\nname = \"x\"\n", TOML, 4, false},   // a future dot
		{"version = \"one\"\n", TOML, 1, false},           // not a number
		{"name = \n", TOML, 0, true},                      // malformed
	}
	for _, c := range cases {
		got, err := Probe([]byte(c.data), c.f)
		if (err != nil) != c.bad || (!c.bad && got != c.want) {
			t.Errorf("Probe(%q, %s) = %d, %v; want %d (error %v)", c.data, c.f, got, err, c.want, c.bad)
		}
	}
}

func TestRunUpgradesOldFilesBacksThemUpAndIsIdempotent(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	old := filepath.Join(state, "widgets", "a.json")
	mid := filepath.Join(state, "widgets", "b.json")
	cur := filepath.Join(state, "widgets", "c.json")
	writeFile(t, old, `{"name":"alpha"}`)
	writeFile(t, mid, `{"version":2,"title":"beta"}`)
	writeFile(t, cur, `{"version":3,"title":"gamma","tags":["x"]}`)
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	rep, err := widgetSet(noLock).Run(Roots{State: state}, Options{Logf: logf})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Migrated) != 2 || len(rep.Backups) != 2 || len(rep.ReadOnly)+len(rep.Failed)+len(rep.Corrupt) != 0 {
		t.Fatalf("report %+v", rep)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(readFile(t, old)), &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != float64(3) || got["title"] != "alpha" || got["name"] != nil || got["tags"] == nil {
		t.Fatalf("a.json after migration: %v", got)
	}
	if b := readFile(t, old+".bak-v1"); b != `{"name":"alpha"}` {
		t.Fatalf("backup holds %q", b)
	}
	platformtest.AssertPrivateFile(t, old+".bak-v1") // 0600, or an owner-only DACL on Windows
	if b := readFile(t, cur); b != `{"version":3,"title":"gamma","tags":["x"]}` {
		t.Fatalf("a current file was rewritten: %s", b)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "upgraded from version 1 to 3") {
		t.Fatalf("logs: %v", logs)
	}

	again, err := widgetSet(noLock).Run(Roots{State: state}, Options{})
	if err != nil || !again.Empty() || len(again.Backups) != 0 {
		t.Fatalf("second run: %+v %v", again, err)
	}
}

func TestNewerFileIsReadOnlyAndNeverRewritten(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	future := filepath.Join(state, "widgets", "f.json")
	writeFile(t, future, `{"version":9,"title":"from 2.0"}`)
	rep, err := widgetSet(noLock).Run(Roots{State: state}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ReadOnly) != 1 || rep.ReadOnly[0] != future || len(rep.Migrated) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if err := fsutil.WriteFileAtomic(future, []byte(`{}`), 0o600); !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("a newer file must stay read-only: %v", err)
	}
	if readFile(t, future) != `{"version":9,"title":"from 2.0"}` {
		t.Fatal("newer file changed")
	}
}

func TestFailedMigrationLeavesTheFileAndMarksItReadOnly(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	s := widgetSet(noLock)
	s.Migrations[1].Apply = func([]byte) ([]byte, error) { return nil, errors.New("boom") }
	rep, err := s.Run(Roots{State: state}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failed) != 1 || !strings.Contains(rep.Failed[0], "version 2 to 3: boom") || len(rep.ReadOnly) != 1 || len(rep.Backups) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if readFile(t, f) != `{"name":"alpha"}` {
		t.Fatal("a failed migration changed the file")
	}
	if _, err := os.Stat(f + ".bak-v1"); !os.IsNotExist(err) {
		t.Fatal("no backup is left behind when nothing was rewritten")
	}
	if !fsutil.Protected(f) {
		t.Fatal("the engine must not write a file it could not migrate")
	}
}

func TestMigrationThatForgetsTheVersionFails(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	s := widgetSet(noLock)
	s.Migrations[0].Apply = func(b []byte) ([]byte, error) { return b, nil }
	rep, _ := s.Run(Roots{State: state}, Options{})
	if len(rep.Failed) != 1 || !strings.Contains(rep.Failed[0], "produced version 1") {
		t.Fatalf("report %+v", rep)
	}
}

func TestCorruptFileIsReportedAndUntouched(t *testing.T) {
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":`)
	rep, _ := widgetSet(noLock).Run(Roots{State: state}, Options{})
	if len(rep.Corrupt) != 1 || len(rep.Migrated)+len(rep.ReadOnly) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if readFile(t, f) != `{"name":` {
		t.Fatal("corrupt file changed")
	}
}

func TestDryRunReportsPendingWithoutWriting(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	rep, _ := widgetSet(noLock).Run(Roots{State: state}, Options{DryRun: true})
	if len(rep.Pending) != 1 || len(rep.Migrated) != 0 || readFile(t, f) != `{"name":"alpha"}` {
		t.Fatalf("report %+v", rep)
	}
}

func TestLockedFileWaitsThenFailsWithoutChange(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	lockPath := filepath.Join(state, "widgets", "widgets.lock")
	held, err := lockfile.TryLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	s := widgetSet(func(string) string { return lockPath })
	start := time.Now()
	rep, _ := s.Run(Roots{State: state}, Options{LockTimeout: 150 * time.Millisecond})
	if time.Since(start) < 150*time.Millisecond || len(rep.Failed) != 1 || !strings.Contains(rep.Failed[0], "lock") {
		t.Fatalf("report %+v after %v", rep, time.Since(start))
	}
	if readFile(t, f) != `{"name":"alpha"}` {
		t.Fatal("file changed without its lock")
	}
	_ = held.Release()
	fsutil.ResetProtected()
	rep, _ = s.Run(Roots{State: state}, Options{})
	if len(rep.Migrated) != 1 {
		t.Fatalf("after the lock is free: %+v", rep)
	}
}

func TestValidateRefusesIncompleteOrUnsafeChains(t *testing.T) {
	gap := widgetSet(noLock)
	gap.Migrations = gap.Migrations[1:]
	unlocked := widgetSet(nil)
	dup := widgetSet(noLock)
	dup.Migrations = append(dup.Migrations, dup.Migrations[0])
	beyond := widgetSet(noLock)
	beyond.Migrations = append(beyond.Migrations, Migration{Kind: "widget", From: 3, Apply: func(b []byte) ([]byte, error) { return b, nil }})
	for name, s := range map[string]Set{"gap": gap, "unlocked shared state": unlocked, "duplicate": dup, "beyond current": beyond} {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", name)
		}
		if _, err := s.Run(Roots{State: t.TempDir()}, Options{}); err == nil {
			t.Errorf("%s: Run accepted it", name)
		}
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default: %v", err)
	}
}

func TestBackupNeverOverwritesAnEarlierBackup(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	writeFile(t, f+".bak-v1", "older backup")
	rep, _ := widgetSet(noLock).Run(Roots{State: state}, Options{})
	if len(rep.Backups) != 1 || rep.Backups[0] != f+".bak-v1-2" {
		t.Fatalf("backups %v", rep.Backups)
	}
	if readFile(t, f+".bak-v1") != "older backup" || !bytes.Equal([]byte(readFile(t, f+".bak-v1-2")), []byte(`{"name":"alpha"}`)) {
		t.Fatal("backups mixed up")
	}
}
