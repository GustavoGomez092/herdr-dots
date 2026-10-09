package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || c != Default() {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestLoadPartialFileMergesOverDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("max_parallel_runs = 5\ninject_enabled = false\n"), 0o600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxParallelRuns != 5 || c.InjectEnabled || c.DefaultBackend != "claude-cli" || c.SessionBudgetCalls != 120 {
		t.Fatalf("merged = %+v", c)
	}
}

func TestLoadClampsNonsense(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("max_parallel_runs = 0\nrun_timeout_seconds = 1\nturn_debounce_ms = -5\nsession_budget_calls = 0\n"), 0o600)
	c, _ := Load(p)
	if c.MaxParallelRuns != 1 || c.RunTimeoutSeconds != 10 || c.TurnDebounceMs != 0 || c.SessionBudgetCalls != 1 {
		t.Fatalf("clamped = %+v", c)
	}
}

// The inject kill switch fails closed: a malformed file disables inject.
func TestLoadBadTOMLReturnsDefaultsWithInjectOffAndError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("inject_enabled = false\nmax_parallel_runs = = 3"), 0o600)
	c, err := Load(p)
	want := Default()
	want.InjectEnabled, want.MemoryEnabled = false, false
	if err == nil || c != want {
		t.Fatalf("want defaults with inject and memory off + error, got %+v %v", c, err)
	}
}

func TestDurations(t *testing.T) {
	c := Default()
	if c.RunTimeout() != 120*time.Second || c.Debounce() != 1500*time.Millisecond {
		t.Fatal("durations")
	}
}
