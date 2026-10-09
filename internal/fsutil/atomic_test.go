package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/platform/platformtest"
)

func TestWriteFileAtomicCreatesWithPerm(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.json")
	if err := WriteFileAtomic(p, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "hello" {
		t.Fatalf("content = %q", got)
	}
	platformtest.AssertPrivateFile(t, p)
	if st, _ := os.Stat(filepath.Dir(p)); !st.IsDir() {
		t.Fatal("parent dir not created")
	}
}

func TestWriteFileAtomicReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	_ = WriteFileAtomic(p, []byte("one"), 0o600)
	_ = WriteFileAtomic(p, []byte("two"), 0o600)
	got, _ := os.ReadFile(p)
	if string(got) != "two" {
		t.Fatalf("content = %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

// The temp file must be private before it holds any secret byte: restrict runs
// right after creation, while the file is still empty.
func TestWriteFileAtomicRestrictsBeforeWriting(t *testing.T) {
	var size int64 = -1
	old := restrictFn
	restrictFn = func(path string, perm os.FileMode) error {
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		size = st.Size()
		return old(path, perm)
	}
	defer func() { restrictFn = old }()
	if err := WriteFileAtomic(filepath.Join(t.TempDir(), "f"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if size != 0 {
		t.Fatalf("restrict ran when the temp file held %d bytes, want 0", size)
	}
}
