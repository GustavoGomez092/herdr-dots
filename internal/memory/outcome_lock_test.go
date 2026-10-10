package memory

import (
	"reflect"
	"testing"
)

// M25: RecordOutcome writes the ledger line and the entry it derives under
// one hold of the project lock, so no other writer (a purge, a delete-dot
// scrub) can run between them. The derived entry is exactly what it was
// before the change: same kind, text, source and evidence.
func TestRecordOutcomeWritesLedgerAndEntryUnderOneLock(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "an earlier model note"))
	before := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	calls := 0
	setHook(t, &lockAcquiredHook, func(string) { calls++ })
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Session: "s1", Pane: "w1:p1", Turn: 4, Card: "3", Kind: OutcomeDismissed, Body: "CSRF tokens on /admin are missing. Details follow."}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("project lock taken %d times, want 1", calls)
	}
	setHook(t, &lockAcquiredHook, nil)
	led, err := s.Ledger(pid)
	if err != nil || len(led) != 1 || led[0].Card != "3" {
		t.Fatalf("ledger %+v %v", led, err)
	}
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != 2 || !reflect.DeepEqual(es[0], before[0]) {
		t.Fatalf("entries %+v", es)
	}
	got := es[1]
	if got.Kind != KindDismissed || got.Text != "CSRF tokens on /admin are missing." || got.Source != SourceOutcome ||
		got.Evidence != (Evidence{Session: "s1", Pane: "w1:p1", Turn: 4, Card: "3"}) || got.Status != StatusActive {
		t.Fatalf("derived entry %+v", got)
	}
}
