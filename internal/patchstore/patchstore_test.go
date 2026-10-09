package patchstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/platform/platformtest"
)

const id1, id2 = "0123456789abcdef", "fedcba9876543210"

func testStore(t *testing.T) *Store {
	t.Helper()
	return Open(filepath.Join(t.TempDir(), "patches"))
}

func TestPutPatchRoundTripsAndVerifiesTheFile(t *testing.T) {
	s := testStore(t)
	diff := []byte("diff --git a/x b/x\n+fixed\n")
	if err := s.PutPatch(Meta{ID: id1, Dot: "security", Pane: "w1:p1", Repo: "/repo", Summary: "Fixed x.", Stat: "1 file +1 −0", Files: 1, Paths: []string{"x"}, Status: StatusReady}, diff); err != nil {
		t.Fatal(err)
	}
	got, m, err := s.Diff(id1)
	if err != nil || string(got) != string(diff) || m.Bytes != len(diff) || m.SHA256 == "" || m.Status != StatusReady || m.CreatedAt.IsZero() {
		t.Fatalf("Diff = %q %+v %v", got, m, err)
	}
	for _, p := range []string{s.PatchPath(id1), filepath.Join(s.Dir, id1+".json")} {
		platformtest.AssertPrivateFile(t, p)
	}
	if err := os.WriteFile(s.PatchPath(id1), []byte("diff --git a/x b/x\n+evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Diff(id1); !errors.Is(err, ErrChanged) {
		t.Fatalf("a swapped patch file: %v, want ErrChanged", err)
	}
}

func TestInvalidIDsNeverTouchTheDisk(t *testing.T) {
	s := testStore(t)
	for _, bad := range []string{"", "../x", "0123456789ABCDEF", "0123456789abcdef/..", "x.json"} {
		if err := s.Put(Meta{ID: bad}); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Put(%q): %v", bad, err)
		}
		if _, err := s.Get(bad); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Get(%q): %v", bad, err)
		}
		if err := s.Delete(bad); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Delete(%q): %v", bad, err)
		}
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatal("refused ids created the directory")
	}
}

func TestUpdateDeleteAndList(t *testing.T) {
	s := testStore(t)
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return clock }
	if err := s.Put(Meta{ID: id2, Status: StatusDrafting}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	if err := s.Put(Meta{ID: id1, Status: StatusDrafting, CreatedAt: clock.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	m, err := s.Update(id2, func(m *Meta) { m.Status, m.Reason, m.ID = StatusRejected, "touches .git", "ignored" })
	if err != nil || m.ID != id2 || m.Status != StatusRejected || !m.UpdatedAt.Equal(clock) {
		t.Fatalf("Update = %+v %v", m, err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "aaaaaaaaaaaaaaaa.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, errs := s.List()
	if len(list) != 2 || list[0].ID != id1 || list[1].ID != id2 || len(errs) != 1 {
		t.Fatalf("List = %+v, %v", list, errs)
	}
	for range 2 {
		if err := s.Delete(id1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Get(id1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete: %v", err)
	}
	if _, err := s.Update(id1, func(*Meta) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update of a deleted patch: %v", err)
	}
}

func TestListOfAMissingDirIsEmpty(t *testing.T) {
	if list, errs := testStore(t).List(); len(list) != 0 || len(errs) != 0 {
		t.Fatalf("%v %v", list, errs)
	}
}
