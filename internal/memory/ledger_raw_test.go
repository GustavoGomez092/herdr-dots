package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 2 VITAL: the ledger only ever gains optional fields, so a newer
// herdr-dots may write lines with fields this version doesn't know. A trim,
// a Forget scrub and a DeleteDot scrub drop exactly the lines they mean to
// and keep every other line byte for byte, unknown fields and key order
// included.
func TestLedgerRewritesKeepUnknownFieldsByteForByte(t *testing.T) {
	s := testStore(t)
	lines := []string{
		`{"at":"2026-10-01T10:00:00Z","dot":"frontend","card":"1","outcome":"sent","body":"Old card.","lane":"a"}`,
		`{"lane":"b","at":"2026-10-02T10:00:00Z","dot":"security","card":"7","outcome":"dismissed","body":"CSRF tokens on /admin are intentionally omitted."}`,
		`{"at":"2026-10-03T10:00:00Z","dot":"security","card":"8","outcome":"sent","body":"Add rate limiting to /login.","weight":{"x":1}}`,
		`{"at":"2026-10-04T10:00:00Z","dot":"frontend","card":"9","outcome":"edited","body":"Use aria labels.","edited":"Use aria-label.","lane":"c"}`,
		`{"at":"2026-10-05T10:00:00Z","dot":"docs","card":"10","outcome":"sent","body":"Document the flag.","lane":"d"}`,
	}
	path := filepath.Join(s.Dir, pid, "ledger.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := func(step string, keep ...int) {
		t.Helper()
		var b strings.Builder
		for _, i := range keep {
			b.WriteString(lines[i] + "\n")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != b.String() {
			t.Fatalf("after %s:\n%s\nwant:\n%s", step, got, b.String())
		}
	}

	s.Limits.MaxLedger = 4
	if rep, err := s.Prune(pid, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)); err != nil || rep.LedgerTrimmed != 1 {
		t.Fatalf("prune %+v %v", rep, err)
	}
	want("a trim", 1, 2, 3, 4)

	e := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindDismissed, Text: "CSRF tokens on /admin are intentionally omitted.", Source: SourceOutcome, Evidence: Evidence{Card: "7"}})
	if err := s.Forget(pid, e.Ref(), true); err != nil {
		t.Fatal(err)
	}
	want("a Forget scrub", 2, 3, 4)

	if _, err := s.DeleteDot("docs"); err != nil {
		t.Fatal(err)
	}
	want("a DeleteDot scrub", 2, 3)

	led, err := s.Ledger(pid)
	if err != nil || len(led) != 2 || led[0].Card != "8" || led[1].Edited != "Use aria-label." {
		t.Fatalf("Ledger reads the lines it kept: %+v %v", led, err)
	}
}
