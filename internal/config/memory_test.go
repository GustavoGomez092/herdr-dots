package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryDefaults(t *testing.T) {
	c := Default()
	if !c.MemoryEnabled || c.MemoryRetentionDays != 180 || c.MemoryMaxEntries != 200 || c.MemoryAutoApproveProject {
		t.Fatalf("%+v", c)
	}
}

// Final review minor 6: memory_enabled fails closed like inject_enabled.
func TestMemoryKillSwitchFailsClosed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("memory_enabled = false\nmax_parallel_runs = = 3"), 0o600)
	if c, err := Load(p); err == nil || c.MemoryEnabled || c.InjectEnabled {
		t.Fatalf("malformed file: want memory and inject off + error, got %+v %v", c, err)
	}
	if c, err := Load(t.TempDir()); err == nil || c.MemoryEnabled || c.InjectEnabled { // a directory can't be read
		t.Fatalf("unreadable file: want memory and inject off + error, got %+v %v", c, err)
	}
}

func TestMemorySettingsOverrideAndClamp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("memory_enabled = false\nmemory_auto_approve_project = true\nmemory_retention_days = 30\nmemory_max_entries = 50\n"), 0o600)
	c, err := Load(p)
	if err != nil || c.MemoryEnabled || !c.MemoryAutoApproveProject || c.MemoryRetentionDays != 30 || c.MemoryMaxEntries != 50 || !c.InjectEnabled {
		t.Fatalf("%+v %v", c, err)
	}
	os.WriteFile(p, []byte("memory_retention_days = 0\nmemory_max_entries = 1\n"), 0o600)
	if c, _ := Load(p); c.MemoryRetentionDays != 1 || c.MemoryMaxEntries != 10 {
		t.Fatalf("low values clamp: %+v", c)
	}
	os.WriteFile(p, []byte("memory_max_entries = 99999\n"), 0o600)
	if c, _ := Load(p); c.MemoryMaxEntries != 10000 {
		t.Fatalf("high values clamp: %+v", c)
	}
}
