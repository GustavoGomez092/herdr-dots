package memory

import "testing"

// Phase 9 residual: in project scope the older approved import goes before
// the one just approved. Two user entries, approved Y, two agent proposals,
// then approve X (limit 5): Y must be archived and X must stay.
func TestOlderApprovedImportGoesBeforeTheOneJustApproved(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxProject = 5
	add := func(src Source, st Status, n int) Entry {
		from := ""
		if src == SourceImport {
			from = "pack"
		}
		return mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindNote, Text: distinct(n), Source: src, Status: st, From: from})
	}
	add(SourceUser, StatusActive, 1)
	add(SourceUser, StatusActive, 2)
	y := add(SourceImport, StatusProposed, 3)
	approve(t, s, y)
	add(SourceModel, StatusProposed, 4)
	add(SourceModel, StatusProposed, 5)
	x := add(SourceImport, StatusProposed, 6)
	approve(t, s, x)
	gotY, _ := s.Get(pid, y.Ref())
	gotX, _ := s.Get(pid, x.Ref())
	if gotY.Status != StatusArchived || gotX.Status != StatusActive {
		t.Fatalf("older Y %s, fresh X %s; want Y archived, X active", gotY.Status, gotX.Status)
	}
}
