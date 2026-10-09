package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteIsOffByDefaultWithTwoDrafts(t *testing.T) {
	if c := Default(); c.WriteEnabled || c.MaxConcurrentDrafts != 2 {
		t.Fatalf("defaults %+v", c)
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("write_enabled = true\nmax_concurrent_drafts = 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || !c.WriteEnabled || c.MaxConcurrentDrafts != 8 {
		t.Fatalf("got %+v %v; want write on, drafts clamped to 8", c, err)
	}
	if err := os.WriteFile(p, []byte("max_concurrent_drafts = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(p); c.MaxConcurrentDrafts != 1 {
		t.Fatalf("drafts clamped to 1, got %d", c.MaxConcurrentDrafts)
	}
}

func TestMalformedConfigTurnsWriteOff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("write_enabled = true\nmax_parallel_runs = = 3"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(p); err == nil || c.WriteEnabled {
		t.Fatalf("a malformed config must fail closed: %+v %v", c, err)
	}
	dir := t.TempDir() // a directory can't be read as a file
	if c, err := Load(dir); err == nil || c.WriteEnabled {
		t.Fatalf("an unreadable config must fail closed: %+v %v", c, err)
	}
}
