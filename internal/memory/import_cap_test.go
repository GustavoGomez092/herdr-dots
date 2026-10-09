package memory

import (
	"fmt"
	"testing"
)

// P9-R15: someone else's approved conclusions never push the developer's own
// outcome or user entries out of a full scope.

// everyStatus lists the security dot's entries, archived ones included.
var everyStatus = Filter{Scope: ScopeDot, Dot: "security", Statuses: []Status{StatusActive, StatusProposed, StatusArchived}}

func dismiss(t *testing.T, s *Store, i int) {
	t.Helper()
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Card: fmt.Sprint(i), Kind: OutcomeDismissed, Body: distinct(i) + "."}); err != nil {
		t.Fatal(err)
	}
}

func importAndApprove(t *testing.T, s *Store, from, n int) {
	t.Helper()
	var in []Entry
	for i := range n {
		in = append(in, Entry{Scope: ScopeDot, Kind: KindNote, Text: distinct(from + i)})
	}
	if res, err := s.ImportPack(pid, "security", in); err != nil || res.Added != n {
		t.Fatalf("res %+v err %v", res, err)
	}
	for _, e := range list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Statuses: []Status{StatusProposed}}) {
		if e.Source == SourceImport {
			approve(t, s, e)
		}
	}
}

// localArchivedWhileImportsRemain fails when an outcome or user entry is
// archived while an approved import is still active to evict instead.
func localArchivedWhileImportsRemain(t *testing.T, s *Store, step string) {
	t.Helper()
	es := list(t, s, pid, everyStatus)
	lostLocal, imports := 0, 0
	for _, e := range es {
		if e.Status == StatusArchived && (e.Source == SourceOutcome || e.Source == SourceUser) {
			lostLocal++
		}
		if e.Status == StatusActive && e.Source == SourceImport {
			imports++
		}
	}
	if lostLocal > 0 && imports > 0 {
		t.Fatalf("%s: %d local entries archived while %d approved imports remain: %+v", step, lostLocal, imports, es)
	}
}

// The reviewer's probe: five dismissals, a pack of five approved, then one
// new outcome.
func TestApprovedImportsNeverArchiveTheDevelopersDismissals(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxDot = 5
	for i := range 5 {
		dismiss(t, s, i)
	}
	importAndApprove(t, s, 100, 5)
	localArchivedWhileImportsRemain(t, s, "after approving")
	for _, e := range list(t, s, pid, everyStatus) {
		if e.Source == SourceOutcome && e.Status != StatusActive {
			t.Fatalf("approving archived a dismissal: %+v", e)
		}
	}
	dismiss(t, s, 50)
	localArchivedWhileImportsRemain(t, s, "after a new outcome")
}

// Within the cap, approved imports are the first active entries to go when a
// new local entry needs room, fresh as they are.
func TestEnforceCapArchivesApprovedImportsBeforeLocalEntries(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxDot = 5
	for i := range 3 {
		dismiss(t, s, i)
	}
	importAndApprove(t, s, 100, 2)
	mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: distinct(60), Source: SourceUser})
	dismiss(t, s, 61)
	localArchivedWhileImportsRemain(t, s, "after two local adds")
	active := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Statuses: []Status{StatusActive}})
	if len(active) != 5 {
		t.Fatalf("active %d, want 5: %+v", len(active), active)
	}
	for _, e := range active {
		if e.Source == SourceImport {
			t.Fatalf("an approved import outlived a local entry: %+v", e)
		}
	}
}

// Approving an import in a full scope must not take it over the cap: Update
// enforces it, and the approved import yields rather than a local entry.
func TestUpdateEnforcesTheCapWithoutEvictingLocalEntries(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxDot = 5
	for i := range 5 {
		dismiss(t, s, i)
	}
	importAndApprove(t, s, 100, 1)
	live := 0
	for _, e := range list(t, s, pid, everyStatus) {
		if e.Status != StatusArchived {
			live++
		}
		if e.Source == SourceOutcome && e.Status != StatusActive {
			t.Fatalf("approving archived a dismissal: %+v", e)
		}
	}
	if live > 5 {
		t.Fatalf("approving took the scope over its cap: %d live", live)
	}
}

// Research still goes first, then approved imports, then the rest.
func TestEnforceCapOrderResearchThenImportsThenRest(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxDot = 4
	for i := range 2 {
		dismiss(t, s, i)
	}
	importAndApprove(t, s, 100, 1)
	research := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindResearch, Text: distinct(70), Source: SourceModel})
	dismiss(t, s, 71)
	if got, _ := s.Get(pid, research.Ref()); got.Status != StatusArchived {
		t.Fatalf("research should go first: %+v", got)
	}
	dismiss(t, s, 72)
	localArchivedWhileImportsRemain(t, s, "after the second outcome")
	for _, e := range list(t, s, pid, everyStatus) {
		if e.Source == SourceOutcome && e.Status != StatusActive {
			t.Fatalf("a dismissal went before the approved import: %+v", e)
		}
	}
}
