package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Ruling P10-R7 (L20): an invalid tool_deny_globs entry fails closed. Load
// keeps the valid globs, records the invalid ones, and RepoToolsOff reports
// it, so repository tools, drafts and research stay off until it is fixed.
// The file is otherwise good, so Load returns no error.
func TestInvalidDenyGlobFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("tool_deny_globs = [\"secrets/**\", \"[unclosed\"]\nwrite_enabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.RepoToolsOff() || !slices.Equal(c.InvalidDenyGlobs, []string{"[unclosed"}) {
		t.Fatalf("RepoToolsOff=%v invalid=%q, want true and the bad glob", c.RepoToolsOff(), c.InvalidDenyGlobs)
	}
	if !slices.Equal(c.ToolDenyGlobs, []string{"secrets/**"}) {
		t.Fatalf("valid globs = %q", c.ToolDenyGlobs)
	}
	if err := os.WriteFile(path, []byte("tool_deny_globs = [\"secrets/**\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(path); c.RepoToolsOff() || len(c.InvalidDenyGlobs) != 0 {
		t.Fatalf("a valid list: RepoToolsOff=%v invalid=%q", c.RepoToolsOff(), c.InvalidDenyGlobs)
	}
	if Default().RepoToolsOff() {
		t.Fatal("the defaults keep repository tools on")
	}
}
