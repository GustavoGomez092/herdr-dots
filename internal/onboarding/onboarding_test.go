package onboarding

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/platform/platformtest"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func store(t *testing.T) Store {
	t.Helper()
	return Store{Path: filepath.Join(t.TempDir(), "cfg", "onboarding.json"), Now: func() time.Time { return t0 }}
}

func TestAMissingRecordIsTheFirstRun(t *testing.T) {
	s := store(t)
	r, exists, err := s.Load()
	if err != nil || exists {
		t.Fatalf("missing file: exists=%v err=%v", exists, err)
	}
	if !r.Asks(MCP, t0) || !r.Asks(Keys, t0) {
		t.Fatal("the first run asks about both")
	}
	if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
		t.Fatal("loading must not create the file")
	}
}

func TestSnoozeAsksAgainAfterSevenDays(t *testing.T) {
	s := store(t)
	if err := s.Mark(Snoozed, MCP, Keys); err != nil {
		t.Fatal(err)
	}
	r, exists, err := s.Load()
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	if r.MCP.State != Snoozed || r.MCP.Until != t0.Add(7*24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("mcp %+v", r.MCP)
	}
	if r.Asks(MCP, t0.Add(7*24*time.Hour-time.Second)) || r.Asks(Keys, t0.Add(24*time.Hour)) {
		t.Fatal("a snoozed item is not asked before 7 days")
	}
	if !r.Asks(MCP, t0.Add(7*24*time.Hour)) || !r.Asks(Keys, t0.Add(8*24*time.Hour)) {
		t.Fatal("a snoozed item is asked again after 7 days")
	}
}

func TestNeverIsNeverAskedAndMarkTouchesOnlyItsItems(t *testing.T) {
	s := store(t)
	if err := s.Mark(Snoozed, Keys); err != nil {
		t.Fatal(err)
	}
	if err := s.Mark(Never, MCP); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if r.MCP.State != Never || r.MCP.Until != "" || r.Asks(MCP, t0.Add(1000*24*time.Hour)) {
		t.Fatalf("never: %+v", r.MCP)
	}
	if r.Keys.State != Snoozed {
		t.Fatalf("keys changed to %+v", r.Keys)
	}
}

func TestInstalledIsRecorded(t *testing.T) {
	s := store(t)
	if err := s.Mark(Installed, MCP); err != nil {
		t.Fatal(err)
	}
	r, _, _ := s.Load()
	if r.MCP.State != Installed || r.Keys.State != "" {
		t.Fatalf("record %+v", r)
	}
}

func TestTheFileIsVersionedPrivateJSON(t *testing.T) {
	s := store(t)
	if err := s.Mark(Snoozed, MCP); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["version"] != float64(1) {
		t.Fatalf("version: %s", b)
	}
	mcp, _ := raw["mcp"].(map[string]any)
	if mcp["state"] != "snoozed" || mcp["until"] != "2026-10-17T12:00:00Z" {
		t.Fatalf("mcp: %s", b)
	}
	platformtest.AssertPrivateFile(t, s.Path)
}

func TestANewerRecordIsNeverRewritten(t *testing.T) {
	s := store(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	newer := `{"version":2,"mcp":{"state":"wizard"}}`
	if err := os.WriteFile(s.Path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load(); !errors.Is(err, ErrNewer) || !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("load: %v", err)
	}
	if err := s.Mark(Never, MCP); !errors.Is(err, ErrNewer) {
		t.Fatalf("mark: %v", err)
	}
	if b, _ := os.ReadFile(s.Path); string(b) != newer {
		t.Fatalf("rewritten: %s", b)
	}
}

func TestACorruptRecordIsNotOverwritten(t *testing.T) {
	s := store(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load(); err == nil {
		t.Fatal("a corrupt record must be an error, never a first run")
	}
	if err := s.Mark(Never, MCP); err == nil {
		t.Fatal("a corrupt record must not be overwritten")
	}
	if b, _ := os.ReadFile(s.Path); string(b) != "{nope" {
		t.Fatalf("rewritten: %s", b)
	}
}

// The record is written under <file>.lock (the migrate kind's lock).
func TestMarkWaitsForTheLock(t *testing.T) {
	s := store(t)
	s.LockTimeout = 50 * time.Millisecond
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := lockAt(s.Path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if err := s.Mark(Never, MCP); err == nil {
		t.Fatal("Mark must not write while another process holds the lock")
	}
	if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
		t.Fatal("nothing written while locked")
	}
}
