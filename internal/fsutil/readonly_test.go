package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedFileRefusesAtomicWritesAndKeepsItsBytes(t *testing.T) {
	t.Cleanup(ResetProtected)
	dir := t.TempDir()
	p := filepath.Join(dir, "queue.json")
	if err := WriteFileAtomic(p, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ProtectNewer(filepath.Join(dir, ".", "queue.json")) // any spelling of the path
	err := WriteFileAtomic(p, []byte(`{}`), 0o600)
	if !errors.Is(err, ErrNewerVersion) {
		t.Fatalf("write to a protected file: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != `{"version":2}` {
		t.Fatalf("protected file changed: %s", b)
	}
	if err := WriteFileAtomic(filepath.Join(dir, "other.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("other files stay writable: %v", err)
	}
	ResetProtected()
	if err := CheckWritable(p); err != nil {
		t.Fatalf("after reset: %v", err)
	}
}
