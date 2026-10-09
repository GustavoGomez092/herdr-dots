package config

import (
	"os"
	"path/filepath"
	"testing"
)

// project_dots_enabled defaults on, can be turned off, and fails closed.
func TestProjectDotsSwitch(t *testing.T) {
	if !Default().ProjectDotsEnabled {
		t.Fatal("project dots are on by default (each file still needs trust)")
	}
	dir := t.TempDir()
	off := filepath.Join(dir, "off.toml")
	os.WriteFile(off, []byte("project_dots_enabled = false\n"), 0o600)
	if c, err := Load(off); err != nil || c.ProjectDotsEnabled {
		t.Fatalf("off: %+v %v", c, err)
	}
	bad := filepath.Join(dir, "bad.toml")
	os.WriteFile(bad, []byte("project_dots_enabled = = true\n"), 0o600)
	if c, err := Load(bad); err == nil || c.ProjectDotsEnabled {
		t.Fatalf("a malformed file must turn project dots off: %+v %v", c, err)
	}
	unreadable := filepath.Join(dir, "dir.toml")
	os.Mkdir(unreadable, 0o700)
	if c, err := Load(unreadable); err == nil || c.ProjectDotsEnabled {
		t.Fatalf("an unreadable file must turn project dots off: %+v %v", c, err)
	}
}
