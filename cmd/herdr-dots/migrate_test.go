package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/migrate"
	"herdr-dots/internal/paths"
)

func migratePaths(t *testing.T) paths.Paths {
	t.Helper()
	base := t.TempDir()
	p := paths.New(filepath.Join(base, "config"), filepath.Join(base, "state"), "/tmp/herdr-test.sock")
	for _, d := range []string{p.ConfigDir, p.SessionDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateCommandReportsAndSkipsRunningSessions(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.ConfigFile(), "default_backend = \"claude-cli\"\n")
	writeTestFile(t, p.QueueFile(), `{"version":2,"cards":[]}`) // from a newer herdr-dots
	other := filepath.Join(p.StateDir, "0123456789ab")
	writeTestFile(t, filepath.Join(other, "queue.json"), `{"next":0,"cards":[]}`)
	held, err := lockfile.TryLock(filepath.Join(other, "engine.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	var out bytes.Buffer
	if err := migrateCmd([]string{"--dry-run"}, p, &out); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "read-only 1") || !strings.Contains(got, "queue.json") {
		t.Fatalf("the newer queue must be reported:\n%s", got)
	}
	if !strings.Contains(got, "skipped 1 running session(s) (0123456789ab)") {
		t.Fatalf("a running session must be skipped:\n%s", got)
	}
}

// The session-root kinds (queue, attachments, usage, patch-meta) have no lock
// of their own: engine.lock guards them. The migrate command must hold it
// while it migrates a session, so an engine starting meanwhile can't open
// the files mid-rewrite, and must release it afterwards.
func TestMigrateCommandHoldsTheSessionEngineLock(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.QueueFile(), `{"cards":[]}`) // version 1
	lock := p.EngineLock()
	heldDuringApply := false
	set := migrate.Set{
		Kinds: []migrate.Kind{{Name: "queue", Root: migrate.SessionRoot, Globs: []string{"queue.json"}, Format: migrate.JSON, Current: 2}},
		Migrations: []migrate.Migration{{Kind: "queue", From: 1, Apply: func([]byte) ([]byte, error) {
			heldDuringApply = lockfile.Held(lock)
			return []byte(`{"version":2,"cards":[]}`), nil
		}}},
	}
	var out bytes.Buffer
	if err := migrateWith(set, nil, p, &out); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	if !heldDuringApply {
		t.Fatal("engine.lock must be held while a session is migrated")
	}
	if lockfile.Held(lock) {
		t.Fatal("engine.lock must be released after the session is migrated")
	}
	if !strings.Contains(out.String(), "migrated 1") || !strings.Contains(out.String(), "backup: ") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// credentials.toml holds keys: a parse error quotes the offending line, so
// neither the command nor the engine log may ever repeat it.
func TestMigrateNeverPrintsCredentialsContents(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	const secret = "sk-ant-THISMUSTNEVERLEAK0123456789"
	writeTestFile(t, filepath.Join(p.ConfigDir, "credentials.toml"), "[anthropic]\napi_key = \""+secret+"\n")
	var out bytes.Buffer
	if err := migrateCmd(nil, p, &out); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	var logs []string
	_, _ = startupMigrations(p, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	all := out.String() + strings.Join(logs, "\n")
	if !strings.Contains(all, "credentials.toml") {
		t.Fatalf("the unreadable file should still be named:\n%s", all)
	}
	if strings.Contains(all, secret) || strings.Contains(all, "api_key") {
		t.Fatalf("credentials contents leaked:\n%s", all)
	}
}

func TestReadOnlyNoticeNamesTheFiles(t *testing.T) {
	if readOnlyNotice(migrate.Report{}) != "" {
		t.Fatal("no notice without read-only files")
	}
	n := readOnlyNotice(migrate.Report{ReadOnly: []string{"/s/queue.json", "/c/dots/security.toml"}})
	if !strings.Contains(n, "2 file(s)") || !strings.Contains(n, "queue.json, security.toml") {
		t.Fatalf("notice %q", n)
	}
	// Final review Minor 8: a failed migration is read-only too, so the
	// toast doesn't blame a newer version alone.
	if !strings.Contains(n, "written by a newer herdr-dots, or could not be migrated; see engine.log") {
		t.Fatalf("notice %q must name both reasons and engine.log", n)
	}
}

func TestWritingCommandsProtectNewerFiles(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.QueueFile(), `{"version":3,"cards":[]}`)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", p.ConfigDir)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", p.StateDir)
	t.Setenv("HERDR_SOCKET_PATH", p.SocketPath)
	protectNewerFiles("pane")
	if fsutil.Protected(p.QueueFile()) {
		t.Fatal("the pane writes no state: nothing to protect")
	}
	protectNewerFiles("manage")
	if !fsutil.Protected(p.QueueFile()) {
		t.Fatal("manage must not overwrite a newer queue")
	}
}

// Every process that writes persisted files outside the engine runs the gate
// first: credentials (keys), dots edit/import, setup-mcp, the memory
// commands, the manage UI and uninstall-extras.
func TestEveryWritingCommandRunsTheGate(t *testing.T) {
	p := migratePaths(t)
	creds := filepath.Join(p.ConfigDir, "credentials.toml")
	writeTestFile(t, creds, "version = 2\n[anthropic]\napi_key = \"x\"\n")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", p.ConfigDir)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", p.StateDir)
	t.Setenv("HERDR_SOCKET_PATH", p.SocketPath)
	for _, cmd := range []string{"manage", "keys", "dots", "setup-mcp", "memory", "uninstall-extras"} {
		fsutil.ResetProtected()
		protectNewerFiles(cmd)
		if !fsutil.Protected(creds) {
			t.Errorf("%s must not overwrite a newer credentials.toml", cmd)
		}
	}
	fsutil.ResetProtected()
}

func TestStartupMigrationsLogsTheSummary(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.UsageFile(), `{"version":5}`)
	var logs []string
	rep, err := startupMigrations(p, func(f string, a ...any) { logs = append(logs, f) })
	if err != nil || len(rep.ReadOnly) != 1 || len(logs) == 0 {
		t.Fatalf("report %+v logs %v err %v", rep, logs, err)
	}
}

// useMigrationSet replaces the set the engine and the CLI gate run.
func useMigrationSet(t *testing.T, s migrate.Set) {
	t.Helper()
	old := migrationSet
	migrationSet = func() migrate.Set { return s }
	t.Cleanup(func() { migrationSet = old })
}

// invalidSet fails Set.Validate: version 2 with no migration from 1.
func invalidSet() migrate.Set {
	return migrate.Set{Kinds: []migrate.Kind{{Name: "queue", Root: migrate.SessionRoot, Globs: []string{"queue.json"}, Format: migrate.JSON, Current: 2}}}
}

func setPluginEnv(t *testing.T, p paths.Paths) {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", p.ConfigDir)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", p.StateDir)
	t.Setenv("HERDR_SOCKET_PATH", p.SocketPath)
}

// Fail closed: an engine whose migration set is broken can't tell which
// files are newer, so it refuses to start rather than run unprotected.
func TestEngineRefusesToStartWhenTheMigrationSetIsInvalid(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	useMigrationSet(t, invalidSet())
	// No Herdr socket: if serveEngine got past the migrations it would stop
	// at herdr.NewFromEnv with a different error, never reach a real Herdr.
	t.Setenv("HERDR_SOCKET_PATH", "")
	err := serveEngine(p)
	if err == nil || !strings.Contains(err.Error(), "schema migrations") {
		t.Fatalf("serveEngine must refuse to start: %v", err)
	}
	if lockfile.Held(p.EngineLock()) {
		t.Fatal("engine.lock must be released")
	}
}

// The CLI gate fails closed too, and dispatch runs it before every writing
// command: the command never runs unprotected.
func TestDispatchRunsTheGateBeforeAWritingCommand(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.QueueFile(), `{"version":3,"cards":[]}`)
	setPluginEnv(t, p)
	ran, protected := false, false
	commands["test-writer"] = func([]string) error { ran, protected = true, fsutil.Protected(p.QueueFile()); return nil }
	writingCommands["test-writer"] = true
	t.Cleanup(func() { delete(commands, "test-writer"); delete(writingCommands, "test-writer") })
	if err := dispatch([]string{"test-writer"}); err != nil {
		t.Fatal(err)
	}
	if !ran || !protected {
		t.Fatalf("the gate must mark the newer queue before the command runs (ran %v, protected %v)", ran, protected)
	}
	fsutil.ResetProtected()
	ran = false
	useMigrationSet(t, invalidSet())
	if err := dispatch([]string{"test-writer"}); err == nil || ran {
		t.Fatalf("a broken migration set must refuse the command: err %v, ran %v", err, ran)
	}
}

// Every command is classified: it runs the gate, or it is listed as writing
// nothing, writing through the engine, or writing only self-guarded or
// unversioned files. A new command fails here until someone decides.
func TestEveryCommandIsClassifiedForTheGate(t *testing.T) {
	for name := range commands {
		if writingCommands[name] == (gateExempt[name] != "") {
			t.Errorf("command %q: must be in exactly one of writingCommands and gateExempt", name)
		}
	}
}

// `migrate` reports a state dir it can't read as a failure, never as
// "all files current".
func TestMigrateReportsAnUnreadableStateDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 doesn't make a directory unreadable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	if err := os.Chmod(p.StateDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p.StateDir, 0o700) })
	var out bytes.Buffer
	err := migrateCmd(nil, p, &out)
	if err == nil {
		t.Fatalf("an unreadable state dir must fail:\n%s", out.String())
	}
	if strings.Contains(out.String(), "all files current") || !strings.Contains(out.String(), "failed 1") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// A dry run writes nothing, not even an engine.lock in a session dir that
// has none.
func TestMigrateDryRunCreatesNoEngineLock(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.QueueFile(), `{"version":2,"cards":[]}`)
	var out bytes.Buffer
	if err := migrateCmd([]string{"--dry-run"}, p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "read-only 1") {
		t.Fatalf("the session must still be checked:\n%s", out.String())
	}
	if _, err := os.Lstat(p.EngineLock()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a dry run must not create engine.lock: %v", err)
	}
}

type blockingNotifier struct{ called chan struct{} }

func (b blockingNotifier) Notify(ctx context.Context, title, body string) error {
	close(b.called)
	<-ctx.Done()
	return ctx.Err()
}

// A slow Herdr socket can't hold up the read-only toast forever: it gives up
// after its timeout and logs why.
func TestReadOnlyToastGivesUpOnASlowHerdr(t *testing.T) {
	n := blockingNotifier{called: make(chan struct{})}
	var logs []string
	start := time.Now()
	notifyReadOnly(context.Background(), n, "2 file(s) ...", 50*time.Millisecond, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("notify took %v", el)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "notify") {
		t.Fatalf("logs %v", logs)
	}
}
