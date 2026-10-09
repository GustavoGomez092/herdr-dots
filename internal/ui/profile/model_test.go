package profile

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

var pNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

const pPID = "g-0123456789abcdef"

// pdrive runs cmd like the Bubble Tea runtime, batches included, and stops at
// a message for the parent.
func pdrive(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Msg) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case BackMsg, ExportMsg, MemoryMsg:
			return m, msg
		case nil:
		default:
			var next tea.Cmd
			m, next = m.Update(msg)
			queue = append(queue, next)
		}
	}
	return m, nil
}

func pkey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func ppress(t *testing.T, m Model, k string) (Model, tea.Msg) {
	t.Helper()
	m, cmd := m.Update(pkey(k))
	return pdrive(t, m, cmd)
}

func fixture(t *testing.T) (*history.Store, *memory.Store) {
	t.Helper()
	dir := t.TempDir()
	hs := history.Open(filepath.Join(dir, "history"))
	day := 24 * time.Hour
	recs := []history.Record{
		{T: history.TypeRun, At: pNow.Add(-20 * day), Dot: "security", Kind: history.KindReview, Outcome: state.OutcomeFlagged, Severity: "warn", Card: "s/1", Excerpt: "Old finding.", Ms: 9000, CostUSD: 0.5},
		{T: history.TypeResolve, At: pNow.Add(-20 * day), Dot: "security", Card: "s/1", Resolution: history.ResSent},
		{T: history.TypeRun, At: pNow.Add(-2 * day), Dot: "security", Kind: history.KindCouncil, Outcome: state.OutcomePass, Ms: 5000, CostUSD: 0.25},
		{T: history.TypeRun, At: pNow.Add(-time.Hour), Dot: "security", Kind: history.KindReview, Outcome: state.OutcomeFlagged, Severity: "critical", Card: "s/2", Excerpt: "CSRF on /admin is open.", Ms: 7000, CostUSD: 0.25},
		{T: history.TypeResolve, At: pNow.Add(-time.Hour), Dot: "security", Card: "s/2", Resolution: history.ResDismissed},
		{T: history.TypeRun, At: pNow.Add(-time.Hour), Dot: "frontend", Kind: history.KindReview, Outcome: state.OutcomePass, CostUSD: 3},
	}
	for _, r := range recs {
		if err := hs.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	st := memory.Open(filepath.Join(dir, "memory"), memory.DefaultLimits())
	st.SeeProject(memory.Project{ID: pPID, Name: "app", Root: "/app"})
	for _, e := range []memory.Entry{
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindFinding, Text: "Rate limit added.", Source: memory.SourceOutcome},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindDismissed, Text: "CSRF fine.", Source: memory.SourceUser, Pinned: true},
	} {
		if _, err := st.Add(pPID, e); err != nil {
			t.Fatal(err)
		}
	}
	return hs, st
}

func openProfile(t *testing.T, cardBody func(string) (string, bool)) Model {
	t.Helper()
	hs, st := fixture(t)
	m := New(Deps{History: hs, Memory: st, Now: func() time.Time { return pNow }, CardBody: cardBody, Purpose: "You review security.\nMore lines.",
		Dot: view.LibraryDot{Slug: "security", Name: "Security", Glyph: "◆", Color: "red", Backend: "claude-cli", Model: "sonnet", Mode: "queue", Origin: "template"}})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = pdrive(t, m, m.Init())
	return m
}

func TestProfileShowsStatsMemoryAndHistory(t *testing.T) {
	m := openProfile(t, nil)
	v := m.View()
	for _, want := range []string{
		"SECURITY · claude-cli/sonnet · queue", "origin: template", "Purpose: You review security.…",
		"LAST 30 DAYS", "runs 3 (review 2 · council 1)", "flagged 67%",
		"accepted 50% (1 / 2)", "cost $1.00", "median 7.0 s",
		"1 project · 2 entries (1 finding · 1 dismissed) · 1 pinned",
		"review", "critical", "dismissed", "CSRF on /admin is open.",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("profile missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "HINTS") {
		t.Fatalf("no hint applies to this fixture:\n%s", v)
	}
	m, _ = ppress(t, m, "tab")
	if v := m.View(); !strings.Contains(v, "LAST 7 DAYS") || !strings.Contains(v, "runs 2 (council 1 · review 1)") || !strings.Contains(v, "accepted 0% (0 / 1)") {
		t.Fatalf("7-day view:\n%s", v)
	}
}

func TestEnterShowsTheFullMessage(t *testing.T) {
	full := "CSRF on /admin is open. " + strings.Repeat("Explain the attack and the fix. ", 20)
	m := openProfile(t, func(key string) (string, bool) { return full, key == "s/2" })
	m, _ = ppress(t, m, "enter")
	if got := strings.Join(strings.Fields(m.View()), " "); !strings.Contains(got, strings.Join(strings.Fields(full), " ")) {
		t.Fatalf("detail must show the whole card:\n%s", m.View())
	}
	m, _ = ppress(t, m, "esc")
	m, _ = ppress(t, m, "j")
	m, _ = ppress(t, m, "j")
	m, _ = ppress(t, m, "enter")
	if v := m.View(); !strings.Contains(v, "Old finding.") || !strings.Contains(v, "first 300 characters") {
		t.Fatalf("an old run shows its stored excerpt:\n%s", v)
	}
}

func TestKeysSendExportMemoryAndBack(t *testing.T) {
	m := openProfile(t, nil)
	if _, msg := ppress(t, m, "x"); msg != (ExportMsg{Slug: "security"}) {
		t.Fatalf("x: %#v", msg)
	}
	if _, msg := ppress(t, m, "M"); msg != (MemoryMsg{Slug: "security", Name: "Security"}) {
		t.Fatalf("M: %#v", msg)
	}
	if _, msg := ppress(t, m, "esc"); msg != (BackMsg{}) {
		t.Fatalf("esc: %#v", msg)
	}
	m.d.Dot.Origin = "project"
	if m2, msg := ppress(t, m, "x"); msg != nil || !strings.Contains(m2.View(), "shared through their repository") {
		t.Fatalf("project dot export: %#v", msg)
	}
}

func TestHintsTable(t *testing.T) {
	base := func() history.Stats { return history.Stats{Runs: map[string]int{}} }
	cases := []struct {
		name  string
		st    func() history.Stats
		share float64
		want  string
	}{
		{"mostly dismissed", func() history.Stats { s := base(); s.Resolved, s.Accepted = 10, 1; return s }, 0, "Mostly dismissed."},
		{"rarely speaks", func() history.Stats { s := base(); s.Verdicts, s.Flagged = 50, 1; return s }, 0, "Rarely speaks."},
		{"expensive", func() history.Stats { s := base(); s.CostUSD = 1; return s }, 0.5, "Your most expensive dot."},
		{"repeats", func() history.Stats { s := base(); s.Suppressed = 5; return s }, 0, "Keeps repeating dismissed advice."},
		{"fails", func() history.Stats { s := base(); s.Runs["review"], s.Errors = 10, 2; return s }, 0, "Fails often."},
	}
	for _, c := range cases {
		hs := Hints(c.st(), c.share)
		if len(hs) != 1 || !strings.HasPrefix(hs[0], c.want) {
			t.Errorf("%s: %v", c.name, hs)
		}
	}
	quiet := []struct {
		name  string
		st    func() history.Stats
		share float64
	}{
		{"9 resolved", func() history.Stats { s := base(); s.Resolved = 9; return s }, 0},
		{"49 verdicts", func() history.Stats { s := base(); s.Verdicts = 49; return s }, 0},
		{"cheap but all of it", func() history.Stats { s := base(); s.CostUSD = 0.99; return s }, 1},
		{"4 suppressed", func() history.Stats { s := base(); s.Suppressed = 4; return s }, 0},
		{"9 runs failing", func() history.Stats { s := base(); s.Runs["review"], s.Errors = 9, 9; return s }, 0},
	}
	for _, c := range quiet {
		if hs := Hints(c.st(), c.share); len(hs) != 0 {
			t.Errorf("%s: %v", c.name, hs)
		}
	}
}
