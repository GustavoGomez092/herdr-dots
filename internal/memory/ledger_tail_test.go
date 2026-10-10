package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// M19: a crash can leave a half-written last ledger line. The next outcome
// starts on a fresh line, so only the crash's own line is unreadable and the
// new line (and the entry it derives) is kept.
func TestLedgerAppendAfterAHalfWrittenLineKeepsTheNewLine(t *testing.T) {
	s := testStore(t)
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Session: "s1", Card: "1", Kind: OutcomeSent, Body: "Add rate limiting to /login."}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, pid, "ledger.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"at":"2026-10-01T00:00:00Z","dot":"secu`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Session: "s1", Card: "2", Kind: OutcomeDismissed, Body: "CSRF tokens on /admin are intentionally omitted."}); err != nil {
		t.Fatal(err)
	}
	led, err := s.Ledger(pid)
	if err != nil || len(led) != 2 || led[0].Card != "1" || led[1].Card != "2" {
		t.Fatalf("ledger %+v %v", led, err)
	}
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != 2 || es[0].Kind != KindFinding || es[1].Kind != KindDismissed {
		t.Fatalf("entries %+v", es)
	}
}
