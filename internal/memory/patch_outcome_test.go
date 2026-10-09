package memory

import (
	"testing"
)

func TestAppliedPatchBecomesAFindingAndDiscardIsLedgerOnly(t *testing.T) {
	s := testStore(t)
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Card: "4", Kind: OutcomeApplied, Body: "Moved the JWT to an httpOnly cookie. Added SameSite=Lax.", Turn: 31, Session: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Card: "5", Kind: OutcomeDiscarded, Body: "Rewrote the session store."}); err != nil {
		t.Fatal(err)
	}
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != 1 || es[0].Kind != KindFinding || es[0].Text != "Applied patch: Moved the JWT to an httpOnly cookie." || es[0].Source != SourceOutcome {
		t.Fatalf("entries %+v, want one outcome finding", es)
	}
	if es[0].Evidence != (Evidence{Session: "s1", Turn: 31, Card: "4"}) {
		t.Fatalf("evidence %+v", es[0].Evidence)
	}
	l, _ := s.Ledger(pid)
	if len(l) != 2 || l[0].Kind != OutcomeApplied || l[1].Kind != OutcomeDiscarded {
		t.Fatalf("ledger %+v", l)
	}
}

func TestAppliedPatchWithASecretLeavesNoEntry(t *testing.T) {
	s := testStore(t)
	body := "Set the key to sk-ant-api03-" + "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ."
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Card: "6", Kind: OutcomeApplied, Body: body}); err != nil {
		t.Fatal(err)
	}
	if es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(es) != 0 {
		t.Fatalf("a secret-looking patch summary must not become memory: %+v", es)
	}
	if l, _ := s.Ledger(pid); len(l) != 1 || l[0].Body != redacted {
		t.Fatalf("ledger %+v", l)
	}
}
