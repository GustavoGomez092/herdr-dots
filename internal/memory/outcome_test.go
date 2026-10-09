package memory

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRecordOutcomeDerivesEntries(t *testing.T) {
	s := testStore(t)
	for _, o := range []Outcome{
		{Dot: "security", Card: "1", Kind: OutcomeSent, Body: "Add rate limiting to /login. Use a token bucket.", Turn: 3, Session: "s1"},
		{Dot: "security", Card: "2", Kind: OutcomeEdited, Body: "Use cookies.", Edited: "Store the session token in an httpOnly cookie. Never localStorage.", Turn: 5},
		{Dot: "security", Card: "3", Kind: OutcomeDismissed, Body: "CSRF tokens on /admin are missing.\nDetails follow.", Severity: "warn"},
	} {
		if err := s.RecordOutcome(pid, o); err != nil {
			t.Fatal(err)
		}
	}
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(es) != 3 {
		t.Fatalf("%+v", es)
	}
	want := []struct {
		kind Kind
		text string
	}{
		{KindFinding, "Add rate limiting to /login."},
		{KindFinding, "Store the session token in an httpOnly cookie."},
		{KindDismissed, "CSRF tokens on /admin are missing."},
	}
	for i, w := range want {
		if es[i].Kind != w.kind || es[i].Text != w.text || es[i].Source != SourceOutcome {
			t.Errorf("entry %d = %+v, want %v %q", i, es[i], w.kind, w.text)
		}
	}
	if es[0].Evidence != (Evidence{Session: "s1", Turn: 3, Card: "1"}) {
		t.Errorf("evidence %+v", es[0].Evidence)
	}
	led, _ := s.Ledger(pid)
	if len(led) != 3 || led[1].Kind != OutcomeEdited || led[1].Edited == "" || led[2].Severity != "warn" || led[0].At.IsZero() {
		t.Fatalf("ledger %+v", led)
	}
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Kind: "approved", Body: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown outcome: %v", err)
	}
}

// Review Focus 1: a card that quotes a secret must not put it on disk.
func TestRecordOutcomeNeverStoresSecrets(t *testing.T) {
	s := testStore(t)
	body := "Don't commit the key AKIAIOSFODNN7EXAMPLE to the repo."
	for _, kind := range []string{OutcomeDismissed, OutcomeSent} {
		if err := s.RecordOutcome(pid, Outcome{Dot: "security", Card: "9", Kind: kind, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	if es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(es) != 0 {
		t.Fatalf("secret became memory: %+v", es)
	}
	led, _ := s.Ledger(pid)
	for _, o := range led {
		if strings.Contains(o.Body, "AKIA") || o.Body != redacted {
			t.Fatalf("ledger kept the secret: %+v", o)
		}
	}
}

func TestRecordOutcomeClipsLedgerBody(t *testing.T) {
	s := testStore(t)
	s.RecordOutcome(pid, Outcome{Dot: "security", Kind: OutcomeDismissed, Body: strings.Repeat("long text ", 100)})
	led, _ := s.Ledger(pid)
	if len([]rune(led[0].Body)) > 300 {
		t.Fatalf("ledger body %d runes", len([]rune(led[0].Body)))
	}
}

// Ledger text came from a model: terminal escapes must not reach disk.
func TestRecordOutcomeSanitizesLedgerText(t *testing.T) {
	s := testStore(t)
	esc := "\x1b[31mred\x1b[0m"
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Kind: OutcomeEdited, Body: "Fix " + esc + " thing.", Edited: "Edited " + esc + " thing."}); err != nil {
		t.Fatal(err)
	}
	led, _ := s.Ledger(pid)
	if len(led) != 1 || strings.ContainsRune(led[0].Body, 0x1b) || strings.ContainsRune(led[0].Edited, 0x1b) {
		t.Fatalf("escape sequences stored: %#v", led)
	}
}

func TestRememberEachKind(t *testing.T) {
	s := testStore(t)
	body := "JWT in localStorage. Move it to a cookie."
	for _, in := range []RememberInput{
		{As: AsFinding, Dot: "security", Text: body},
		{As: AsRule, Dot: "security", Text: body, Reason: " internal\ntool "},
		{As: AsConvention, Dot: "security", Text: "Tokens live in httpOnly cookies."},
		{As: AsGlobal, Dot: "security", Text: "Answer in British English."},
	} {
		if _, err := s.Remember(pid, in); err != nil {
			t.Fatalf("%s: %v", in.As, err)
		}
	}
	d := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(d) != 2 || d[0].Kind != KindFinding || d[0].Text != "JWT in localStorage." || d[1].Kind != KindDismissed ||
		d[1].Text != "JWT in localStorage."+ReasonSep+"internal tool)" || d[1].Source != SourceUser {
		t.Fatalf("dot %+v", d)
	}
	p := list(t, s, pid, Filter{Scope: ScopeProject})
	if len(p) != 1 || p[0].Status != StatusActive || p[0].Dot != "" || p[0].Kind != KindConvention {
		t.Fatalf("project %+v", p)
	}
	g := list(t, s, "", Filter{Scope: ScopeGlobal, Dot: "security"})
	if len(g) != 1 || g[0].Kind != KindPreference {
		t.Fatalf("global %+v", g)
	}
	if _, err := s.Remember(pid, RememberInput{As: AsFinding, Dot: "security", Text: "key AKIAIOSFODNN7EXAMPLE"}); !errors.Is(err, ErrSecret) {
		t.Fatalf("secret: %v", err)
	}
	if _, err := s.Remember(pid, RememberInput{As: "forever", Dot: "security", Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad As: %v", err)
	}
}

func TestPruneArchivesStaleDeletesOldAndTrimsLedger(t *testing.T) {
	s := testStore(t)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return t0 }
	mustAdd(t, s, pid, dotNote("security", "stale model fact"))
	mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "old developer fact", Source: SourceUser})
	mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "old pinned model fact", Source: SourceModel, Pinned: true})
	s.Now = func() time.Time { return t0.Add(170 * 24 * time.Hour) }
	mustAdd(t, s, pid, dotNote("security", "recent model fact"))
	s.Limits.MaxLedger = 4
	for i := 0; i < 10; i++ {
		s.RecordOutcome(pid, Outcome{Dot: "frontend", Kind: OutcomeDismissed, Body: "card number " + strings.Repeat("i", i+1)})
	}
	rep, err := s.Prune(pid, t0.Add(200*24*time.Hour))
	if err != nil || rep.Archived != 1 || rep.Deleted != 0 || rep.LedgerTrimmed != 6 {
		t.Fatalf("%+v %v", rep, err)
	}
	live := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(live) != 3 {
		t.Fatalf("live %+v", live)
	}
	for _, e := range live {
		if e.Text == "stale model fact" {
			t.Fatal("stale model entry not archived")
		}
	}
	rep, err = s.Prune(pid, t0.Add(291*24*time.Hour))
	if err != nil || rep.Deleted != 1 {
		t.Fatalf("archived entries are deleted after 90 days: %+v %v", rep, err)
	}
	led, _ := s.Ledger(pid)
	if len(led) != 4 || !strings.HasSuffix(led[3].Body, strings.Repeat("i", 10)) {
		t.Fatalf("ledger keeps the newest lines: %+v", led)
	}
	if rep, err := s.Prune("p-00000000000000aa", t0); err != nil || rep != (PruneReport{}) {
		t.Fatalf("a project without memory: %+v %v", rep, err)
	}
}
