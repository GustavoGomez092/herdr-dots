package config

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzConfigLoad checks the kill-switch rule on every input: a file that
// fails to load turns inject and memory off, and a loaded file is clamped.
func FuzzConfigLoad(f *testing.F) {
	f.Add("default_backend = \"claude-cli\"\ninject_enabled = true\nmemory_enabled = true\n")
	f.Add("max_parallel_runs = -4\nrun_timeout_seconds = 1\nmemory_max_entries = 99999999\n[council]\nrounds = 9\nmax_members = -1\n")
	f.Add("inject_enabled = \"yes\"\n")
	f.Add("[[council]]\n")
	f.Fuzz(func(t *testing.T, data string) {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		if err != nil {
			if c.InjectEnabled || c.MemoryEnabled {
				t.Fatalf("a config that failed to load left a kill switch on: %+v", c)
			}
			return
		}
		if c.MaxParallelRuns < 1 || c.RunTimeoutSeconds < 10 || c.SessionBudgetCalls < 1 || c.TurnDebounceMs < 0 ||
			c.MemoryMaxEntries < 10 || c.MemoryMaxEntries > 10000 || c.Council.Rounds < 1 || c.Council.Rounds > 2 ||
			c.Council.MaxMembers < 2 || c.Council.MaxMembers > 16 {
			t.Fatalf("value out of range after Load: %+v", c)
		}
	})
}
