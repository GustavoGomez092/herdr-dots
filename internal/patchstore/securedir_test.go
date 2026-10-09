package patchstore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The patches directory names files and holds diffs of the user's code: it
// is made private (platform.SecureDir) even when it already existed wider.
func TestThePatchesDirectoryIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses ACLs; covered by platform's own tests")
	}
	for name, put := range map[string]func(*Store) error{
		"Put": func(s *Store) error { return s.Put(Meta{ID: "0123456789abcdef", Status: StatusDrafting}) },
		"PutPatch": func(s *Store) error {
			return s.PutPatch(Meta{ID: "0123456789abcdef", Status: StatusReady}, []byte("diff"))
		},
	} {
		dir := filepath.Join(t.TempDir(), "patches")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := put(Open(dir)); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode().Perm(); got != 0o700 {
			t.Errorf("%s: patches dir mode %o, want 700", name, got)
		}
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			fi, _ := e.Info()
			if fi.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s: %s mode %o", name, e.Name(), fi.Mode().Perm())
			}
		}
	}
}
