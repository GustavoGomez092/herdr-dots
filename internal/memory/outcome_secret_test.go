package memory

import "testing"

// A caller that redacted a secret before the text got here says so: the
// ledger line is redacted and no entry is made, as when ScanSecrets finds one.
func TestAnOutcomeMarkedSecretLeavesARedactedLineAndNoEntry(t *testing.T) {
	s := testStore(t)
	for _, kind := range []string{OutcomeSent, OutcomeApplied, OutcomeDiscarded} {
		if err := s.RecordOutcome(pid, Outcome{Dot: "security", Card: "1", Kind: kind, Body: "Rotated sk-ant-[redacted] out of the config.", Secret: true}); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := s.Ledger(pid)
	if len(l) != 3 {
		t.Fatalf("ledger %+v", l)
	}
	for _, o := range l {
		if o.Body != redacted {
			t.Fatalf("ledger line %+v, want %q", o, redacted)
		}
	}
	if es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(es) != 0 {
		t.Fatalf("entries %+v", es)
	}
}
