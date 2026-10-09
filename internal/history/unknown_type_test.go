package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/state"
)

// A record type a newer herdr-dots added is skipped, not counted as
// unreadable, and survives a trim byte for byte.
func TestUnknownRecordTypeIsSkippedNotCounted(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s, runRec("security", KindReview, state.OutcomeFlagged, "s/1", time.Hour))
	path := filepath.Join(s.Dir, "security.jsonl")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"t":"lane","at":"2026-10-08T12:00:00Z","dot":"security","lane":"a"}` + "\n")
	f.Close()
	st, err := s.Stats("security", hNow, 24*time.Hour)
	if err != nil || st.TotalRuns() != 1 || st.Skipped != 0 {
		t.Fatalf("stats %+v %v", st, err)
	}
}

// A file holding only records of types this version doesn't know is not
// damaged: an older binary's writer must not quarantine it.
func TestFileOfOnlyNewerRecordTypesIsNotQuarantined(t *testing.T) {
	s := hstore(t)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, "security.jsonl")
	newer := `{"t":"lane","at":"2026-10-08T12:00:00Z","dot":"security","lane":"a"}` + "\n"
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, runRec("security", KindReview, state.OutcomePass, "", time.Minute))
	if aside, _ := filepath.Glob(path + ".corrupt-*"); len(aside) != 0 {
		t.Fatalf("a file of newer records was quarantined: %v", aside)
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) < len(newer) || string(b[:len(newer)]) != newer {
		t.Fatalf("the newer record must stay first and unchanged: %q %v", b, err)
	}
}
