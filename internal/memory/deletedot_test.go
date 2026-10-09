package memory

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDeleteDotScrubsItsLedgerAndOrphanedProposals(t *testing.T) {
	s := testStore(t)
	other := "p-00000000000000ff"
	for _, p := range []string{pid, other} {
		if err := s.RecordOutcome(p, Outcome{Dot: "security", Card: "1", Kind: OutcomeSent, Body: "Rotate the JWT secret."}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordOutcome(pid, Outcome{Dot: "frontend", Card: "2", Kind: OutcomeDismissed, Body: "Add a loading skeleton."}); err != nil {
		t.Fatal(err)
	}
	mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "security", Kind: KindConvention, Text: "Tokens live in httpOnly cookies.", Status: StatusProposed, Source: SourceModel})
	mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "security", Kind: KindDecision, Text: "Sessions expire after one hour.", Status: StatusArchived, Source: SourceModel})
	approved := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "security", Kind: KindConvention, Text: "Every handler checks CSRF tokens.", Status: StatusActive, Source: SourceModel})
	mine := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindConvention, Text: "Use pnpm, never npm."})
	theirs := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "frontend", Kind: KindConvention, Text: "Components use CSS modules.", Status: StatusProposed, Source: SourceModel})
	agent, err := s.Propose(pid, Entry{Kind: KindConvention, Text: "Run make lint before pushing."}) // Phase 5: the main agent's, no dot
	if err != nil {
		t.Fatal(err)
	}

	del, err := s.DeleteDot("security")
	if err != nil {
		t.Fatal(err)
	}
	if want := (DotDeletion{Files: 2, LedgerLines: 2, Proposals: 2}); del != want {
		t.Fatalf("DeleteDot = %+v, want %+v", del, want)
	}
	if l, _ := s.Ledger(pid); len(l) != 1 || l[0].Dot != "frontend" {
		t.Fatalf("ledger of %s = %+v, want only frontend's line", pid, l)
	}
	if l, _ := s.Ledger(other); len(l) != 0 {
		t.Fatalf("ledger of %s = %+v, want empty", other, l)
	}
	var ids []string
	for _, e := range list(t, s, pid, Filter{Scope: ScopeProject, Statuses: []Status{StatusActive, StatusProposed, StatusArchived}}) {
		ids = append(ids, e.ID)
	}
	if want := []string{approved.ID, mine.ID, theirs.ID, agent.ID}; !slices.Equal(ids, want) {
		t.Fatalf("project entries %v, want %v (approved, developer, other dots' and the agent's entries stay)", ids, want)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, other, "project.json")); !os.IsNotExist(err) {
		t.Fatal("DeleteDot created a project file where there was none")
	}
	if del, err := s.DeleteDot("security"); err != nil || del != (DotDeletion{}) {
		t.Fatalf("second DeleteDot = %+v, %v; want nothing left", del, err)
	}
}

func TestDeleteDotKeepsGoingPastADamagedProjectFile(t *testing.T) {
	s := testStore(t)
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Card: "1", Kind: OutcomeSent, Body: "Rotate the JWT secret."}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, pid, "project.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	del, err := s.DeleteDot("security")
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err %v, want ErrCorrupt reported", err)
	}
	if del.Files != 1 || del.LedgerLines != 1 {
		t.Fatalf("DeleteDot = %+v: the dot file and ledger lines must still go", del)
	}
	if got, _ := os.ReadFile(filepath.Join(s.Dir, pid, "project.json")); string(got) != "{" {
		t.Fatal("a damaged project file must be left for the developer, not rewritten")
	}
}

func TestDotDeletionString(t *testing.T) {
	for d, want := range map[DotDeletion]string{
		{}:                                       "0 files",
		{Files: 3}:                               "3 files",
		{Files: 1, LedgerLines: 1, Proposals: 1}: "1 file, 1 ledger line, 1 proposal",
		{Files: 2, LedgerLines: 4, Proposals: 2}: "2 files, 4 ledger lines, 2 proposals",
	} {
		if got := d.String(); got != want {
			t.Errorf("%+v = %q, want %q", d, got, want)
		}
	}
}
