package memory

import (
	"path/filepath"
	"reflect"
	"testing"
)

func appendLines(t *testing.T, s *Store, os ...Outcome) {
	t.Helper()
	dir := filepath.Join(s.Dir, pid)
	for _, o := range os {
		if err := s.withLock(dir, func() error { return s.appendLedgerLocked(pid, o) }); err != nil {
			t.Fatal(err)
		}
	}
}

func cards(t *testing.T, s *Store) []string {
	t.Helper()
	led, err := s.Ledger(pid)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, o := range led {
		out = append(out, o.Dot+"/"+o.Session+"/"+o.Card)
	}
	return out
}

// M17: card ids restart in every Herdr session, so a scrub matches a card id
// only within the entry's session (and, for a project entry a dot proposed,
// only that dot's lines). Unrelated cards that reuse the id keep their lines.
// Forget still deletes only the one entry; the others are untouched.
func TestForgetScrubMatchesACardOnlyInItsSessionAndDot(t *testing.T) {
	s := testStore(t)
	appendLines(t, s,
		Outcome{Dot: "security", Session: "s1", Card: "7", Kind: OutcomeSent, Body: "Unrelated card from an earlier session."},
		Outcome{Dot: "security", Session: "s2", Card: "7", Kind: OutcomeDismissed, Body: "CSRF tokens on /admin are intentionally omitted."},
		Outcome{Dot: "frontend", Session: "s2", Card: "3", Kind: OutcomeSent, Body: "Buttons need an accessible label."},
		Outcome{Dot: "security", Session: "s2", Card: "3", Kind: OutcomeSent, Body: "Handlers must use the repository layer."},
	)
	dotEntry := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindDismissed, Text: "CSRF tokens on /admin are intentionally omitted.", Source: SourceOutcome, Evidence: Evidence{Session: "s2", Card: "7"}})
	projEntry := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "security", Kind: KindConvention, Text: "Database access goes through repositories.", Source: SourceModel, Status: StatusProposed, Evidence: Evidence{Session: "s2", Card: "3"}})
	other := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindFinding, Text: "Add rate limiting to /login.", Source: SourceUser})

	if err := s.Forget(pid, dotEntry.Ref(), true); err != nil {
		t.Fatal(err)
	}
	if got, want := cards(t, s), []string{"security/s1/7", "frontend/s2/3", "security/s2/3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after dot forget: ledger %v, want %v", got, want)
	}
	if err := s.Forget(pid, projEntry.Ref(), true); err != nil {
		t.Fatal(err)
	}
	if got, want := cards(t, s), []string{"security/s1/7", "frontend/s2/3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after project forget: ledger %v, want %v", got, want)
	}
	got, err := s.Get(pid, other.Ref())
	if err != nil || !reflect.DeepEqual(got, other) {
		t.Fatalf("an unrelated entry changed: %+v %v", got, err)
	}
	if n := len(list(t, s, pid, Filter{Statuses: []Status{StatusActive, StatusProposed, StatusArchived}})); n != 1 {
		t.Fatalf("%d entries left, want 1", n)
	}
}

// M17: an "Applied patch: …" finding merges every card that applied the same
// patch; a scrub also removes the lines of the earlier cards, not only the
// card in its evidence.
func TestForgetScrubRemovesEveryAppliedPatchLineItMerged(t *testing.T) {
	s := testStore(t)
	body := "Use parameterized queries in db/users.go. The rest of the patch follows."
	for _, c := range []string{"5", "9"} {
		if err := s.RecordOutcome(pid, Outcome{Dot: "security", Session: "s1", Card: c, Kind: OutcomeApplied, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	appendLines(t, s, Outcome{Dot: "security", Session: "s1", Card: "11", Kind: OutcomeSent, Body: "Rotate the signing key yearly."})
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != 1 || es[0].Evidence.Card != "9" {
		t.Fatalf("entries %+v", es)
	}
	if err := s.Forget(pid, es[0].Ref(), true); err != nil {
		t.Fatal(err)
	}
	if got, want := cards(t, s), []string{"security/s1/11"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ledger %v, want %v", got, want)
	}
}
