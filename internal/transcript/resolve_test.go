package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

// T2 (post-rc hardening): a glob metacharacter in the home path ("[" in a
// user name) doesn't make the transcript lookup miss.
func TestResolveClaudePathUnderAHomeWithGlobCharacters(t *testing.T) {
	home := filepath.Join(t.TempDir(), "us[er]*?")
	dir := filepath.Join(home, ".claude", "projects", "-r")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Skipf("this file system refuses the name: %v", err)
	}
	f := filepath.Join(dir, "abc-123.jsonl")
	if err := os.WriteFile(f, []byte(claudeSample), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, err := ResolveClaudePath(home, "id", "abc-123"); err != nil || p != f {
		t.Fatalf("by id: %q %v", p, err)
	}
}

// T3 (post-rc hardening): a "path" session that names a directory is
// refused at resolve time (the reader falls back to scrollback at once).
func TestResolveClaudePathRefusesADirectory(t *testing.T) {
	if _, err := ResolveClaudePath(t.TempDir(), "path", t.TempDir()); err == nil {
		t.Fatal("a directory is not a transcript")
	}
}

// T7 (post-rc hardening): Select without a pane reader doesn't panic later;
// the scrollback reader it falls back to reports an error instead.
func TestSelectWithoutAPaneReaderDoesNotPanic(t *testing.T) {
	r := Select(SelectOptions{Agent: "claude"})
	if _, err := r.Turns(t.Context()); err == nil {
		t.Fatal("a scrollback reader without a pane must fail, not succeed")
	}
}
