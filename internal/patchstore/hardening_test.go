package patchstore

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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

// D4: the paths the Phase 7 tests left out.
func TestDiffOfAMissingPatchAnEmptyHashAndAnInvalidID(t *testing.T) {
	s := testStore(t)
	if err := s.Put(Meta{ID: id1, Status: StatusRejected}); err != nil { // a record with no patch file
		t.Fatal(err)
	}
	if _, _, err := s.Diff(id1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Diff with no patch file: %v, want ErrNotFound", err)
	}
	diff := []byte("diff --git a/x b/x\n")
	if err := s.PutPatch(Meta{ID: id2, Status: StatusReady}, diff); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(id2, func(m *Meta) { m.SHA256 = "" }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Diff(id2); !errors.Is(err, ErrChanged) {
		t.Fatalf("Diff with an empty hash: %v, want ErrChanged", err)
	}
	for _, bad := range []string{"", "../x", "0123456789ABCDEF"} {
		if _, _, err := s.Diff(bad); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Diff(%q): %v", bad, err)
		}
		if _, err := s.Update(bad, func(*Meta) {}); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Update(%q): %v", bad, err)
		}
	}
	if _, err := s.Update("00000000000000aa", func(*Meta) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update of a missing record: %v", err)
	}
}

// D4: concurrent Updates never lose one (Update's read-modify-write is
// serialized).
func TestConcurrentUpdatesAreSerialized(t *testing.T) {
	s := testStore(t)
	if err := s.Put(Meta{ID: id1, Status: StatusReady}); err != nil {
		t.Fatal(err)
	}
	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Update(id1, func(m *Meta) { m.Files++; m.Paths = append(m.Paths, strconv.Itoa(i)) }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	m, err := s.Get(id1)
	if err != nil || m.Files != n || len(m.Paths) != n {
		t.Fatalf("after %d concurrent updates: files %d, paths %d, %v", n, m.Files, len(m.Paths), err)
	}
}

func TestSweepOrphanPatchesOfAMissingDir(t *testing.T) {
	if n, err := testStore(t).SweepOrphanPatches(); n != 0 || err != nil {
		t.Fatalf("SweepOrphanPatches = %d, %v", n, err)
	}
}
