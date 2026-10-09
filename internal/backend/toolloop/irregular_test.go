package toolloop

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// irregularInfo is a FileInfo whose mode says "irregular": what Windows
// reports for a junction, mount point or unknown reparse point (Go 1.23+),
// which filepath.EvalSymlinks no longer follows.
type irregularInfo struct{ os.FileInfo }

func (irregularInfo) Mode() fs.FileMode { return fs.ModeIrregular | fs.ModeDir }

// lstatMarking is os.Lstat, except that paths in marked look irregular.
func lstatMarking(marked ...string) func(string) (os.FileInfo, error) {
	return func(p string) (os.FileInfo, error) {
		fi, err := os.Lstat(p)
		for _, m := range marked {
			if err == nil && p == m {
				return irregularInfo{fi}, nil
			}
		}
		return fi, err
	}
}

// Review Focus (Important): a path through a junction (proj\ext -> outside)
// passes EvalSymlinks unchanged; any irregular element under the root must
// be refused, on the found path and on the not-found path alike.
func TestResolveRefusesAnIrregularPathElement(t *testing.T) {
	root := t.TempDir()
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(rr, "ext", "notes.txt"), "outside")
	mustWrite(t, filepath.Join(rr, "src", "a.go"), "package a")
	for _, p := range []string{"ext/notes.txt", "ext/missing.txt", "ext", filepath.Join(rr, "ext", "notes.txt")} {
		r := newResolver()
		r.lstat = lstatMarking(filepath.Join(rr, "ext"))
		if _, _, err := r.resolve(root, p, DefaultDeny); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("%q: err = %v, want ErrOutsideRoot", p, err)
		}
	}
	r := newResolver()
	r.lstat = lstatMarking(filepath.Join(rr, "ext"))
	if _, rel, err := r.resolve(root, "src/a.go", DefaultDeny); err != nil || rel != "src/a.go" {
		t.Fatalf("a regular path: rel %q err %v", rel, err)
	}
	// The last element itself irregular.
	r = newResolver()
	r.lstat = lstatMarking(filepath.Join(rr, "src", "a.go"))
	if _, _, err := r.resolve(root, "src/a.go", DefaultDeny); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("irregular file: err = %v, want ErrOutsideRoot", err)
	}
}
