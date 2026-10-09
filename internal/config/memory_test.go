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
