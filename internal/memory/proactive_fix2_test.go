package memory

import (
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/backend"
)

// A developer's pinned research entry is never archived, not even to make room.
func TestPinnedUserResearchIsNeverArchivedOnAdd(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxDot = 5
	fillSuppressors(t, s, ScopeDot, 5)
	e := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindResearch, Text: "pinned note", Pinned: true})
	if e.Status != StatusActive {
		t.Fatalf("returned %+v", e)
	}
	got, err := s.Get(pid, e.Ref())
	if err != nil || got.Status != StatusActive {
		t.Fatalf("stored %+v %v", got, err)
	}
	u := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindQuestion, Text: "user question"})
	if got, _ := s.Get(pid, u.Ref()); got.Status != StatusActive {
		t.Fatalf("user research archived: %+v", got)
	}
}

func projectProposals(t *testing.T, s *Store, kind Kind, base, n int) {
	t.Helper()
	for i := range n {
		mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "security", Kind: kind, Text: distinct(base + i), Source: SourceModel, Status: StatusProposed})
	}
}

func statusCount(t *testing.T, s *Store, k Kind, st Status) int {
	t.Helper()
	return len(list(t, s, pid, Filter{Scope: ScopeProject, Kinds: []Kind{k}, Statuses: []Status{st}}))
}

func TestResearchLosesFirstAmongProposals(t *testing.T) {
	t.Run("a research proposal never pushes out a convention proposal", func(t *testing.T) {
		s := testStore(t)
		s.Now = clock()
		s.Limits.MaxProject = 5
		projectProposals(t, s, KindConvention, 0, 5)
		res, err := s.Apply(pid, "security", []backend.MemoryOp{{Op: "add", Scope: "project", Kind: "research", Text: distinct(100)}}, ev, ApplyOptions{Research: true})
		if err != nil || res.Added != 0 || len(res.Rejected) != 1 {
			t.Fatalf("the archived research entry must not count as added: %+v %v", res, err)
		}
		if n := statusCount(t, s, KindConvention, StatusProposed); n != 5 {
			t.Fatalf("%d convention proposals left, want 5", n)
		}
		if n := statusCount(t, s, KindResearch, StatusArchived); n != 1 {
			t.Fatalf("the research entry should be archived: %d", n)
		}
	})
	t.Run("active research with auto-approve archives no non-research proposal", func(t *testing.T) {
		s := testStore(t)
		s.Now = clock()
		s.Limits.MaxProject = 5
		s.Limits.AutoApproveProject = true
		projectProposals(t, s, KindConvention, 0, 5)
		addResearch(t, s, ScopeProject, 100, 3)
		if n := statusCount(t, s, KindConvention, StatusProposed); n != 5 {
			t.Fatalf("%d convention proposals left, want 5", n)
		}
	})
	t.Run("the oldest research proposal goes before a convention proposal", func(t *testing.T) {
		s := testStore(t)
		s.Now = clock()
		s.Limits.MaxProject = 5
		projectProposals(t, s, KindConvention, 0, 1)
		projectProposals(t, s, KindResearch, 100, 4)
		projectProposals(t, s, KindConvention, 200, 1)
		if n := statusCount(t, s, KindConvention, StatusProposed); n != 2 {
			t.Fatalf("%d convention proposals, want 2", n)
		}
		arch := list(t, s, pid, Filter{Scope: ScopeProject, Kinds: []Kind{KindResearch}, Statuses: []Status{StatusArchived}})
		if len(arch) != 1 || arch[0].Text != distinct(100) {
			t.Fatalf("archived %+v, want the oldest research proposal", arch)
		}
	})
}

func TestAddReportsAnArchivedResearchEntryAsArchived(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxDot = 5
	fillSuppressors(t, s, ScopeDot, 5)
	e := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindResearch, Text: "model research", Source: SourceModel})
	if e.Status != StatusArchived {
		t.Fatalf("Add returned %+v", e)
	}
}

func TestAsideMatchUsesTheDotsSeenKeyForm(t *testing.T) {
	s := testStore(t)
	dir := filepath.Join(s.Dir, pid)
	os.MkdirAll(dir, 0o700)
	named := filepath.Join(dir, "meta.json.corrupt-1")
	key := filepath.Join(dir, "meta.json.corrupt-2")
	os.WriteFile(named, []byte(`{"name": "security", "roots": ["/x"], "dots_seen":{"frontend": "2026`), 0o600)
	os.WriteFile(key, []byte(`{"name": "x", "dots_seen":{"security": "2026`), 0o600)
	if _, err := s.DeleteDot("security"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(named); err != nil {
		t.Fatalf("an aside that only has the slug as a value was removed: %v", err)
	}
	if _, err := os.Stat(key); err == nil {
		t.Fatal("the aside with the dots_seen key survived")
	}
}
