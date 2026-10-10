package history

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// H1: when the corrupt file can't be moved aside, the writer reports the error
// and keeps the file; it never falls back to deleting it.
func TestQuarantineKeepsTheFileWhenTheMoveFails(t *testing.T) {
	s := hstore(t)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, "security.jsonl")
	body := []byte("\x00\x01garbage\n{not json\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory at the aside name makes the rename fail on every OS.
	blocker := filepath.Join(fmt.Sprintf("%s.corrupt-%d", path, hNow.UnixNano()), "x")
	if err := os.MkdirAll(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Trim("security"); err == nil {
		t.Fatal("Trim: want the rename error, got nil")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("file not kept intact: %q %v", got, err)
	}
}
