package fsutil

import (
	"os"
	"path/filepath"
	"testing"
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
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", st.Mode().Perm())
	}
	dst, _ := os.Stat(filepath.Dir(p))
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %v", dst.Mode().Perm())
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
