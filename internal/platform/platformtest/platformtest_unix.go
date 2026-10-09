//go:build unix

package platformtest

import (
	"os"
	"path/filepath"
	"testing"
)

const root = "/"

func endpoint(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hdep")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "e.sock")
}

func assertPrivate(t testing.TB, path string, dir bool) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	want := os.FileMode(0o600)
	if dir {
		want = 0o700
	}
	if st.IsDir() != dir || st.Mode().Perm() != want {
		t.Fatalf("%s: mode %v (dir=%v), want %v", path, st.Mode(), st.IsDir(), want)
	}
}
