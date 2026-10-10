package transcript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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

// T3 (post-rc hardening): a cancelled pass doesn't read the transcript.
func TestClaudeReaderHonoursACancelledContext(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(f, []byte(claudeSample), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if turns, err := (ClaudeReader{Path: f}).Turns(ctx); err == nil || turns != nil {
		t.Fatalf("cancelled: %d turns, err %v", len(turns), err)
	}
}

// T4 (post-rc hardening): clipping never splits a multi-byte rune, from the
// start or from the end.
func TestFormatClipsNonASCIIOnRuneBoundaries(t *testing.T) {
	long := strings.Repeat("é日🙂", MaxFieldChars)
	out := Format([]Turn{{Index: 1, User: long, Assistant: long}})
	if !utf8.ValidString(out) {
		t.Fatal("clipped output is not valid UTF-8")
	}
	if !strings.Contains(out, "…") {
		t.Fatal("not clipped")
	}
}
