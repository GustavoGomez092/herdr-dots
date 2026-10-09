package memory

import "testing"

func archivedCount(t *testing.T, s *Store) int {
	t.Helper()
	n := 0
	for _, e := range list(t, s, pid, allProject) {
		if e.Status == StatusArchived {
			n++
		}
	}
	return n
}

func addDot(t *testing.T, s *Store, e Entry, n int) Entry {
	t.Helper()
	e.Scope, e.Text = ScopeProject, distinct(n)
	if e.Kind == "" {
		e.Kind = KindNote
	}
	return mustAdd(t, s, pid, e)
}

// Round 2: the cap never loses two entries for one. Fresh research that
// pushes out older research stays.
func TestFreshResearchStaysWhenTheCapAlreadyEvictedOne(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxProject = 3
	addDot(t, s, Entry{Source: SourceUser}, 1)
	addDot(t, s, Entry{Source: SourceUser}, 2)
	addDot(t, s, Entry{Kind: KindResearch, Source: SourceModel}, 3)
	addDot(t, s, Entry{Source: SourceModel, Status: StatusProposed}, 4)
	fresh := addDot(t, s, Entry{Kind: KindResearch, Source: SourceModel}, 5)
	if got := archivedCount(t, s); got != 1 {
		t.Fatalf("archived %d, want exactly 1: %+v", got, list(t, s, pid, allProject))
	}
	if e, _ := s.Get(pid, fresh.Ref()); e.Status != StatusActive {
		t.Fatalf("fresh research was archived: %+v", e)
	}
}

// The same with an approved import and a pending one approved.
func TestFreshlyApprovedImportStaysWhenTheCapAlreadyEvictedOne(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxProject = 3
	addDot(t, s, Entry{Source: SourceUser}, 1)
	addDot(t, s, Entry{Source: SourceUser}, 2)
	a := addDot(t, s, Entry{Source: SourceImport, Status: StatusProposed, From: "pack"}, 3)
	approve(t, s, a)
	addDot(t, s, Entry{Source: SourceModel, Status: StatusProposed}, 4)
	x := addDot(t, s, Entry{Source: SourceImport, Status: StatusProposed, From: "pack"}, 5)
	approve(t, s, x)
	if got := archivedCount(t, s); got != 1 {
		t.Fatalf("archived %d, want exactly 1: %+v", got, list(t, s, pid, allProject))
	}
	if e, _ := s.Get(pid, x.Ref()); e.Status != StatusActive {
		t.Fatalf("fresh import was archived: %+v", e)
	}
}

// And when the limit was lowered.
func TestLoweredLimitArchivesOneNotTwo(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxProject = 4
	addDot(t, s, Entry{Source: SourceUser}, 1)
	addDot(t, s, Entry{Source: SourceUser}, 2)
	y := addDot(t, s, Entry{Source: SourceImport, Status: StatusProposed, From: "pack"}, 3)
	approve(t, s, y)
	addDot(t, s, Entry{Source: SourceModel, Status: StatusProposed}, 4)
	x := addDot(t, s, Entry{Source: SourceImport, Status: StatusProposed, From: "pack"}, 5)
	s.Limits.MaxProject = 3
	approve(t, s, x)
	if got := archivedCount(t, s); got != 1 {
		t.Fatalf("archived %d, want exactly 1: %+v", got, list(t, s, pid, allProject))
	}
	if e, _ := s.Get(pid, x.Ref()); e.Status != StatusActive {
		t.Fatalf("fresh import was archived: %+v", e)
	}
}

var allProject = Filter{Scope: ScopeProject, Statuses: []Status{StatusActive, StatusProposed, StatusArchived}}
