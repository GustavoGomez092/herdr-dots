package patchstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// D1: a patch file whose size isn't the drafted size is ErrChanged.
func TestDiffOfAFileOfAnotherSizeIsChanged(t *testing.T) {
	s := testStore(t)
	diff := []byte("diff --git a/x b/x\n+fixed\n")
	if err := s.PutPatch(Meta{ID: id1, Status: StatusReady}, diff); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.PatchPath(id1), append(diff, "+more\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Diff(id1); !errors.Is(err, ErrChanged) {
		t.Fatalf("a grown patch file: %v, want ErrChanged", err)
	}
}

// D3: a .patch with no .json (a crash between PutPatch's two writes) is
// swept; a patch with its record, a record alone, other files and an
// unreadable record's patch stay.
func TestSweepOrphanPatchesRemovesOnlyPatchesWithoutARecord(t *testing.T) {
	s := testStore(t)
	if err := s.PutPatch(Meta{ID: id1, Status: StatusReady}, []byte("diff a\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.PatchPath(id2), []byte("diff orphan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const id3 = "00000000000000aa"
	if err := os.WriteFile(s.PatchPath(id3), []byte("diff c\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, id3+".json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(s.Dir, "notes.patch")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := s.SweepOrphanPatches()
	if err != nil || n != 1 {
		t.Fatalf("SweepOrphanPatches = %d, %v; want 1", n, err)
	}
	if _, err := os.Stat(s.PatchPath(id2)); !os.IsNotExist(err) {
		t.Fatalf("the orphan patch stayed: %v", err)
	}
	for _, p := range []string{s.PatchPath(id1), s.PatchPath(id3), other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if _, _, err := s.Diff(id1); err != nil {
		t.Fatal(err)
	}
}

func TestSweepOrphanPatchesOfAMissingDir(t *testing.T) {
	if n, err := testStore(t).SweepOrphanPatches(); n != 0 || err != nil {
		t.Fatalf("SweepOrphanPatches = %d, %v", n, err)
	}
}
