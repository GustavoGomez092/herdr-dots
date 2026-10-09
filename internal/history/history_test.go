package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform/platformtest"
	"herdr-dots/internal/state"
)

var hNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func hstore(t *testing.T) *Store {
	t.Helper()
	s := Open(filepath.Join(t.TempDir(), "history"))
	s.Now = func() time.Time { return hNow }
	return s
}

func runRec(slug, kind, outcome, card string, ago time.Duration) Record {
	return Record{T: TypeRun, At: hNow.Add(-ago), Dot: slug, Kind: kind, Outcome: outcome, Card: card, Pane: "w1:p1", Turn: 3}
}

func resRec(slug, card, res string, ago time.Duration) Record {
	return Record{T: TypeResolve, At: hNow.Add(-ago), Dot: slug, Card: card, Resolution: res}
}

func mustAppend(t *testing.T, s *Store, rs ...Record) {
	t.Helper()
	for _, r := range rs {
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppendQueryNewestFirstWithResolution(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s,
		runRec("security", KindReview, state.OutcomeFlagged, "s1/1", 3*time.Hour),
		runRec("security", KindSummon, state.OutcomeFlagged, "s1/2", 2*time.Hour),
		resRec("security", "s1/1", ResDismissed, time.Hour),
		runRec("security", KindReview, state.OutcomePass, "", 30*time.Minute),
	)
	got, err := s.Query("security", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Outcome != state.OutcomePass || got[2].Card != "s1/1" {
		t.Fatalf("order: %+v", got)
	}
	if got[2].Resolution != ResDismissed || got[1].Resolution != "" {
		t.Fatalf("resolutions: %+v", got)
	}
	limited, _ := s.Query("security", hNow.Add(-150*time.Minute), 1)
	if len(limited) != 1 || limited[0].Outcome != state.OutcomePass {
		t.Fatalf("since/limit: %+v", limited)
	}
}

func TestResolutionJoinsTheLatestRunWithThatCardKey(t *testing.T) {
	s := hstore(t)
	// Card ids restart per session; the key carries the session, and a reused
	// key resolves the most recent run before it.
	mustAppend(t, s,
		runRec("security", KindReview, state.OutcomeFlagged, CardKey("s1", "1"), 3*time.Hour),
		resRec("security", CardKey("s1", "1"), ResSent, 170*time.Minute),
		runRec("security", KindReview, state.OutcomeFlagged, CardKey("s2", "1"), 2*time.Hour),
		resRec("security", CardKey("s2", "1"), ResDismissed, time.Hour),
	)
	got, _ := s.Query("security", time.Time{}, 0)
	if got[0].Resolution != ResDismissed || got[1].Resolution != ResSent {
		t.Fatalf("joins: %+v", got)
	}
	if CardKey("s1", "") != "" {
		t.Fatal("no card, no key")
	}
}

// An inject card is delivered (and resolved) while its run is still
// finishing, so its resolve line comes first.
func TestResolutionWrittenBeforeItsRunStillJoins(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s,
		resRec("fast", "s1/4", ResInjected, time.Minute),
		runRec("fast", KindReview, state.OutcomeFlagged, "s1/4", time.Minute),
	)
	if got, _ := s.Query("fast", time.Time{}, 0); len(got) != 1 || got[0].Resolution != ResInjected {
		t.Fatalf("join: %+v", got)
	}
}

func TestStatsMath(t *testing.T) {
	s := hstore(t)
	day := 24 * time.Hour
	r1 := runRec("security", KindReview, state.OutcomeFlagged, "s/1", day)
	r1.Ms, r1.CostUSD = 8000, 0.5
	r2 := runRec("security", KindReview, state.OutcomePass, "", day)
	r2.Ms, r2.CostUSD = 6000, 0.25
	r3 := runRec("security", KindCouncil, state.OutcomeFlagged, "s/2", 2*day)
	r3.Ms = 10000
	r4 := runRec("security", KindReview, state.OutcomeSuppressed, "", 2*day)
	r4.Ms = 7000
	r5 := runRec("security", KindMemory, state.OutcomePass, "", 2*day)
	r5.Ms = 1000
	r6 := runRec("security", KindReview, state.OutcomeError, "", 3*day)
	r6.Ms, r6.CostUnknown = 99999, true
	old := runRec("security", KindReview, state.OutcomeFlagged, "s/9", 40*day)
	mustAppend(t, s, r1, r2, r3, r4, r5, r6, old,
		resRec("security", "s/1", ResEdited, day),
		resRec("security", "s/2", ResDismissed, day),
		resRec("security", "s/3", ResApplied, day),
		resRec("security", "s/4", ResInjected, day),
		resRec("security", "s/9", ResSent, 39*day),
	)
	st, err := s.Stats("security", hNow, 30*day)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalRuns() != 6 || st.Runs[KindReview] != 4 || st.Runs[KindCouncil] != 1 || st.Runs[KindMemory] != 1 {
		t.Fatalf("runs %+v", st.Runs)
	}
	if st.Verdicts != 4 || st.Flagged != 2 || st.Suppressed != 1 || st.Errors != 1 {
		t.Fatalf("verdicts %+v", st)
	}
	if st.Resolved != 3 || st.Accepted != 2 || st.Dismissed != 1 {
		t.Fatalf("resolutions %+v", st)
	}
	if st.FlaggedRate() != 0.5 || st.AcceptanceRate() != 2.0/3.0 || st.ErrorRate() != 1.0/6.0 {
		t.Fatalf("rates %v %v %v", st.FlaggedRate(), st.AcceptanceRate(), st.ErrorRate())
	}
	if st.CostUSD != 0.75 || st.CostUnknown != 1 {
		t.Fatalf("cost %+v", st)
	}
	// Latencies of non-error runs: 1000 6000 7000 8000 10000 → median 7000.
	if st.MedianMs != 7000 {
		t.Fatalf("median %d", st.MedianMs)
	}
	week, _ := s.Stats("security", hNow, 7*24*time.Hour)
	if week.TotalRuns() != 6 || week.Resolved != 3 {
		t.Fatalf("7d %+v", week)
	}
	empty, _ := s.Stats("nobody", hNow, 30*day)
	if empty.TotalRuns() != 0 || empty.FlaggedRate() != 0 || empty.AcceptanceRate() != 0 || empty.MedianMs != 0 {
		t.Fatalf("empty %+v", empty)
	}
}

func TestCorruptLineIsSkippedAndCounted(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s, runRec("security", KindReview, state.OutcomeFlagged, "s/1", time.Hour))
	f, _ := os.OpenFile(filepath.Join(s.Dir, "security.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	// {"t":""} has no type at all; an unknown non-empty type is a newer
	// record, not an unreadable one (TestUnknownRecordTypeIsSkippedNotCounted).
	f.WriteString("{not json\n{\"t\":\"\"}\n")
	f.Close()
	mustAppend(t, s, runRec("security", KindReview, state.OutcomePass, "", time.Minute))
	st, err := s.Stats("security", hNow, 24*time.Hour)
	if err != nil || st.TotalRuns() != 2 || st.Skipped != 2 {
		t.Fatalf("stats %+v %v", st, err)
	}
}

func TestTrimKeepsTheNewestLinesAndTheirResolutions(t *testing.T) {
	s := hstore(t)
	s.MaxLines, s.TrimTo, s.TrimEvery = 10, 6, 1
	for i := 0; i < 8; i++ {
		mustAppend(t, s, runRec("security", KindReview, state.OutcomeFlagged, CardKey("s", string(rune('a'+i))), time.Duration(20-i)*time.Minute))
	}
	// "a" was run first; its resolution arrives last, after "a" is trimmed.
	mustAppend(t, s,
		resRec("security", CardKey("s", "h"), ResSent, 5*time.Minute),
		resRec("security", CardKey("s", "g"), ResDismissed, 4*time.Minute),
		resRec("security", CardKey("s", "a"), ResSent, 3*time.Minute),
	)
	data, _ := os.ReadFile(filepath.Join(s.Dir, "security.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 10 {
		t.Fatalf("%d lines after trim", len(lines))
	}
	got, _ := s.Query("security", time.Time{}, 0)
	if got[0].Card != CardKey("s", "h") || got[0].Resolution != ResSent || got[1].Resolution != ResDismissed {
		t.Fatalf("kept runs lost their resolutions: %+v", got)
	}
	for _, r := range got {
		if r.Card == CardKey("s", "a") {
			t.Fatal("the oldest run should have been trimmed")
		}
	}
	if err := s.Trim("security"); err != nil {
		t.Fatal(err)
	}
}

func TestAppendRefusesBadRecordsAndCleansExcerpts(t *testing.T) {
	s := hstore(t)
	s.Redact = func(x string) string { return strings.ReplaceAll(x, "sk-SECRET", "[redacted]") } // extra scrubber
	if err := s.Append(Record{T: "other", Dot: "security"}); err == nil {
		t.Fatal("unknown type accepted")
	}
	if err := s.Append(Record{T: TypeRun, Dot: "../etc"}); err == nil {
		t.Fatal("path-like slug accepted")
	}
	long := "\x1b[31mkey sk-SECRET\x1b[0m\n" + strings.Repeat("é", 400)
	r := runRec("security", KindReview, state.OutcomeFlagged, "", time.Minute)
	r.Excerpt = long
	mustAppend(t, s, r)
	got, _ := s.Query("security", time.Time{}, 0)
	ex := got[0].Excerpt
	if strings.Contains(ex, "\x1b") || strings.Contains(ex, "sk-SECRET") || strings.Contains(ex, "\n") {
		t.Fatalf("excerpt not cleaned: %q", ex)
	}
	if n := len([]rune(ex)); n != ExcerptRunes {
		t.Fatalf("excerpt %d runes", n)
	}
}

// With no Redact of its own the store uses credentials.Redact, so a caller
// that forgets to wire one still never writes a key (GitHub, AWS, Slack,
// Stripe, GitLab, URL credentials, provider keys) or a bidi control.
func TestDefaultRedactionNeverStoresKeys(t *testing.T) {
	s := hstore(t)
	r := runRec("security", KindReview, state.OutcomeFlagged, "", time.Minute)
	keys := []string{
		"sk-ant-api03-abcdefghijklmnop",
		"ghp_" + strings.Repeat("a", 36),
		"AKIA" + strings.Repeat("B", 16),
		"xoxb-1234567890-abcdef",
		"sk_live_" + strings.Repeat("c", 24),
		"glpat-" + strings.Repeat("d", 20),
		"https://bob:hunter2pass@example.com/x",
	}
	r.Excerpt = "‮mix " + strings.Join(keys, " ")
	mustAppend(t, s, r)
	data, err := os.ReadFile(filepath.Join(s.Dir, "security.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range append(keys[:6:6], "hunter2pass", "‮", `‮`) {
		if strings.Contains(string(data), k) {
			t.Fatalf("history file holds %q: %s", k, data)
		}
	}
}

func TestCostShareAcrossDots(t *testing.T) {
	s := hstore(t)
	a := runRec("security", KindReview, state.OutcomePass, "", time.Hour)
	a.CostUSD = 3
	b := runRec("frontend", KindReview, state.OutcomePass, "", time.Hour)
	b.CostUSD = 1
	mustAppend(t, s, a, b)
	share, total, err := s.CostShare("security", hNow, 30*24*time.Hour)
	if err != nil || total != 4 || share != 0.75 {
		t.Fatalf("share %v total %v err %v", share, total, err)
	}
	if slugs, _ := s.Slugs(); len(slugs) != 2 {
		t.Fatalf("slugs %v", slugs)
	}
}

func TestProfileReadsBothWindowsAndRowsInOnePass(t *testing.T) {
	s := hstore(t)
	day := 24 * time.Hour
	mustAppend(t, s,
		runRec("security", KindReview, state.OutcomeFlagged, "s/1", 20*day),
		runRec("security", KindReview, state.OutcomeFlagged, "s/2", 2*day),
		resRec("security", "s/2", ResSent, day),
		runRec("security", KindAsk, state.OutcomePass, "", time.Hour),
	)
	p, err := s.Profile("security", hNow, 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Week.TotalRuns() != 2 || p.Month.TotalRuns() != 3 || p.Week.Accepted != 1 {
		t.Fatalf("windows %+v / %+v", p.Week, p.Month)
	}
	if len(p.Rows) != 2 || p.Rows[0].Kind != KindAsk || p.Rows[1].Resolution != ResSent {
		t.Fatalf("rows %+v", p.Rows)
	}
}

func TestFilesArePrivate(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s, runRec("security", KindReview, state.OutcomePass, "", time.Minute))
	platformtest.AssertPrivateFile(t, filepath.Join(s.Dir, "security.jsonl"))
}

// A file with nothing readable is quarantined by a writer, never by a reader.
func TestWhollyCorruptFileIsQuarantinedByAppendNotByReaders(t *testing.T) {
	s := hstore(t)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, "security.jsonl")
	if err := os.WriteFile(path, []byte("\x00\x01garbage\n{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats("security", hNow, time.Hour)
	if err != nil || st.TotalRuns() != 0 || st.Skipped != 2 {
		t.Fatalf("stats %+v %v", st, err)
	}
	s.Query("security", time.Time{}, 0)
	s.Profile("security", hNow, 5)
	s.CostShare("security", hNow, time.Hour)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a reader moved the file: %v", err)
	}
	mustAppend(t, s, runRec("security", KindReview, state.OutcomePass, "", time.Minute))
	aside, _ := filepath.Glob(path + ".corrupt-*")
	if len(aside) != 1 {
		t.Fatalf("corrupt file not moved aside: %v", aside)
	}
	st, _ = s.Stats("security", hNow, time.Hour)
	if st.TotalRuns() != 1 || st.Skipped != 0 {
		t.Fatalf("after recovery: %+v", st)
	}
}

func TestTrimQuarantinesAWhollyCorruptFile(t *testing.T) {
	s := hstore(t)
	os.MkdirAll(s.Dir, 0o700)
	path := filepath.Join(s.Dir, "security.jsonl")
	os.WriteFile(path, []byte("nope\n"), 0o600)
	if err := s.Trim("security"); err != nil {
		t.Fatal(err)
	}
	if aside, _ := filepath.Glob(path + ".corrupt-*"); len(aside) != 1 {
		t.Fatalf("aside %v", aside)
	}
}

// A crash can leave a half-written last line with no newline; the next
// record must start on its own line.
func TestAppendAfterHalfWrittenLastLine(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s, runRec("security", KindReview, state.OutcomeFlagged, "s/1", time.Hour))
	path := filepath.Join(s.Dir, "security.jsonl")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"t":"run","dot":"secur`)
	f.Close()
	mustAppend(t, s, runRec("security", KindReview, state.OutcomePass, "", time.Minute))
	st, err := s.Stats("security", hNow, 24*time.Hour)
	if err != nil || st.TotalRuns() != 2 || st.Skipped != 1 {
		t.Fatalf("stats %+v %v", st, err)
	}
	if got, _ := s.Query("security", time.Time{}, 0); got[0].Outcome != state.OutcomePass {
		t.Fatalf("new record unreadable: %+v", got)
	}
}

// Keys never reach the file, in any free-text field, whatever Redact is wired.
func TestSecretsNeverReachTheFileInAnyField(t *testing.T) {
	key := "sk-ant-api03-abcdefghijklmnop"
	for name, redact := range map[string]func(string) string{
		"nil": nil, "noop": func(x string) string { return x },
	} {
		s := hstore(t)
		s.Redact = redact
		r := runRec("security", KindReview, state.OutcomeFlagged, CardKey("s", key), time.Minute)
		r.Excerpt, r.Project, r.Pane = "a "+key, "/p/"+key, "w:"+key
		mustAppend(t, s, r)
		mustAppend(t, s, Record{T: TypeResolve, Dot: "security", Card: CardKey("s", key), Resolution: ResSent})
		data, _ := os.ReadFile(filepath.Join(s.Dir, "security.jsonl"))
		if strings.Contains(string(data), "abcdefghijklmnop") {
			t.Fatalf("%s: key stored: %s", name, data)
		}
	}
}

func TestAppendRefusesUnknownEnumValues(t *testing.T) {
	s := hstore(t)
	for _, r := range []Record{
		{T: TypeRun, Dot: "security", Kind: "sk-ant-api03-abcdefghijklmnop"},
		{T: TypeRun, Dot: "security", Outcome: "bogus"},
		{T: TypeRun, Dot: "security", Severity: "sk-ant-api03-abcdefghijklmnop"},
		{T: TypeResolve, Dot: "security", Resolution: "bogus"},
	} {
		if err := s.Append(r); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "security.jsonl")); err == nil {
		t.Fatal("a refused record wrote a file")
	}
	for _, out := range []string{"flagged", "pass", "error", "budget", "learned", "suppressed", "council", "research", "scheduled", "commit", "drafted", "applied", "asked", "dropped"} {
		if err := s.Append(Record{T: TypeRun, Dot: "security", Outcome: out, Severity: "warn"}); err != nil {
			t.Fatalf("outcome %q refused: %v", out, err)
		}
	}
}

func TestCostTotalsPerDot(t *testing.T) {
	s := hstore(t)
	a := runRec("security", KindReview, state.OutcomePass, "", time.Hour)
	a.CostUSD = 3
	b := runRec("frontend", KindReview, state.OutcomePass, "", time.Hour)
	b.CostUSD = 1
	old := runRec("frontend", KindReview, state.OutcomePass, "", 40*24*time.Hour)
	old.CostUSD = 9
	mustAppend(t, s, a, b, old)
	got, err := s.CostTotals(hNow, 30*24*time.Hour)
	if err != nil || got["security"] != 3 || got["frontend"] != 1 || len(got) != 2 {
		t.Fatalf("totals %v %v", got, err)
	}
}

func TestLockFileAndDirArePrivate(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s, runRec("security", KindReview, state.OutcomePass, "", time.Minute))
	platformtest.AssertPrivateFile(t, filepath.Join(s.Dir, "security.lock"))
	platformtest.AssertPrivateDir(t, s.Dir)
}
