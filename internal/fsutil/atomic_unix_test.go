//go:build unix

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicCreatesParentWith0700(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "f.json")
	if err := WriteFileAtomic(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Dir(p)); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %v", st.Mode().Perm())
	}
}
