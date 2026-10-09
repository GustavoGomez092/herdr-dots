package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/state"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

const hostile = "漢字漢字漢字漢字 😀😀😀😀 verylongwordwithoutanybreakshereverylongwordwithoutanybreakshere"

// bigModel opens a profile over n runs. Record j carries the label row-JJ
// first, then wide text; the newest (j = n-1) is a card "c/new" with a long body.
func bigModel(t *testing.T, n, w, h int, dot view.LibraryDot, body string) Model {
	t.Helper()
	dir := t.TempDir()
	hs := history.Open(filepath.Join(dir, "history"))
	for j := 0; j < n; j++ {
		r := history.Record{T: history.TypeRun, At: pNow.Add(-time.Duration(n-j) * time.Minute), Dot: "security", Kind: history.KindReview,
			Outcome: state.OutcomeFlagged, Severity: "warn", Excerpt: fmt.Sprintf("row-%02d %s", j, hostile), CostUSD: 0.1}
		if j == n-1 {
			r.Card = "c/new"
		}
		if err := hs.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	m := New(Deps{History: hs, Now: func() time.Time { return pNow }, Purpose: hostile + "\n" + hostile, Dot: dot,
		CardBody: func(k string) (string, bool) { return body, body != "" && k == "c/new" }})
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = pdrive(t, m, m.Init())
	return m
}

var hostileDot = view.LibraryDot{Slug: "security", Name: hostile, Glyph: "◆", Color: "red", Backend: hostile, Model: hostile, Mode: hostile, Origin: hostile}

func assertFits(t *testing.T, label, v string, w, h int) {
	t.Helper()
	lines := strings.Split(v, "\n")
	for _, l := range lines {
		if got := ansi.StringWidth(l); got > w {
			t.Errorf("%s: line is %d cells (> %d): %q", label, got, w, l)
		}
	}
	if h > 0 && len(lines) > h {
		t.Errorf("%s: %d lines (> height %d):\n%s", label, len(lines), h, v)
	}
}

func TestEveryLineFitsTheWidth(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		old := style.SetASCII(ascii)
		for _, w := range []int{40, 60, 80} {
			for _, h := range []int{0, 14, 30} {
				m := bigModel(t, 30, w, h, hostileDot, strings.Repeat("漢字 body ", 80))
				tag := fmt.Sprintf("ascii=%v w=%d h=%d", ascii, w, h)
				assertFits(t, tag+" main", m.View(), w, h)
				m, _ = ppress(t, m, "tab")
				assertFits(t, tag+" week", m.View(), w, h)
				m, _ = ppress(t, m, "x")
				m, _ = ppress(t, m, "enter")
				assertFits(t, tag+" detail", m.View(), w, h)
			}
		}
		style.SetASCII(old)
	}
}

func TestErrorAndLoadingLinesFit(t *testing.T) {
	m := New(Deps{Dot: view.LibraryDot{Slug: "security", Name: "S"}})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	assertFits(t, "loading", m.View(), 40, 20)
	m, _ = pdrive(t, m, m.Init())
	v := m.View()
	if !strings.Contains(strings.Join(strings.Fields(v), " "), "history is not available") {
		t.Fatalf("a nil history store is reported:\n%s", v)
	}
	assertFits(t, "nil history", v, 40, 20)
	m, _ = m.Update(profileMsg{err: errors.New(strings.Repeat("boom ", 40) + "\x1b[31m")})
	assertFits(t, "error", m.View(), 40, 20)
	if strings.Contains(m.View(), "\x1b[31m") {
		t.Fatal("error text is sanitized")
	}
}

func TestCursorRowStaysVisibleAtSmallHeights(t *testing.T) {
	const n = 30
	for _, h := range []int{10, 12, 16, 24} {
		m := bigModel(t, n, 60, h, hostileDot, "")
		for i := 0; i < n; i++ {
			label := fmt.Sprintf("row-%02d", n-1-i)
			var cursorLine string
			for _, l := range strings.Split(m.View(), "\n") {
				if strings.HasPrefix(l, style.Sym("▸")) {
					cursorLine = l
				}
			}
			if !strings.Contains(cursorLine, label) {
				t.Fatalf("h=%d i=%d: cursor row %q not on screen:\n%s", h, i, label, m.View())
			}
			assertFits(t, fmt.Sprintf("h=%d i=%d", h, i), m.View(), 60, h)
			m, _ = ppress(t, m, "j")
		}
	}
}

func TestDetailScrollsToTheEnd(t *testing.T) {
	var sb strings.Builder
	for i := 0; sb.Len() < 5000; i++ {
		fmt.Fprintf(&sb, "w%04d ", i)
	}
	body := strings.TrimSpace(sb.String())
	last := body[strings.LastIndex(body, "w"):]
	const w, h = 50, 16
	m := bigModel(t, 3, w, h, view.LibraryDot{Slug: "security", Name: "Security"}, body)
	m, _ = ppress(t, m, "enter")
	if m.CanAct() {
		t.Fatal("a long card has not been read to the end yet")
	}
	v := m.View()
	if !strings.Contains(v, "w0000") || strings.Contains(v, last) || !strings.Contains(v, "more below") {
		t.Fatalf("top of a long card:\n%s", v)
	}
	m, _ = ppress(t, m, "pgdown")
	if v := m.View(); strings.Contains(v, "w0000") || !strings.Contains(v, "RUN ") {
		t.Fatalf("a page down moves the body but keeps the heading:\n%s", v)
	}
	m, _ = ppress(t, m, "pgup")
	if !strings.Contains(m.View(), "w0000") {
		t.Fatal("page up returns to the top")
	}
	for i := 0; i < 1000 && !m.CanAct(); i++ {
		m, _ = ppress(t, m, "j")
		v := m.View()
		if !strings.Contains(v, "RUN ") {
			t.Fatalf("heading must stay visible:\n%s", v)
		}
		assertFits(t, "detail scroll", v, w, h)
	}
	v = m.View()
	if !m.CanAct() || !strings.Contains(v, last) || strings.Contains(v, "more below") {
		t.Fatalf("end of the card:\n%s", v)
	}
	m, _ = ppress(t, m, "k")
	if !m.CanAct() {
		t.Fatal("having shown the end once, scrolling back up keeps it counted")
	}
	m, _ = ppress(t, m, "esc")
	if m.detail || !m.CanAct() {
		t.Fatal("esc leaves the detail")
	}
}

func TestASCIIModeIsClean(t *testing.T) {
	old := style.SetASCII(true)
	defer style.SetASCII(old)
	m := openProfile(t, func(string) (string, bool) { return "full body", true })
	for _, v := range []string{m.View(), func() string { m, _ = ppress(t, m, "enter"); return m.View() }()} {
		for _, r := range v {
			if r > 127 {
				t.Fatalf("non-ASCII %q in ASCII mode:\n%s", r, v)
			}
		}
	}
}

func TestCursorFollowsTheRecordAcrossARefresh(t *testing.T) {
	m := openProfile(t, nil)
	m, _ = ppress(t, m, "j")
	m, _ = ppress(t, m, "j")
	if m.prof.Rows[m.cursor].Excerpt != "Old finding." {
		t.Fatalf("setup: %#v", m.prof.Rows[m.cursor])
	}
	p := m.prof
	fresh := history.Record{T: history.TypeRun, At: pNow, Dot: "security", Kind: history.KindReview, Excerpt: "brand new"}
	p.Rows = append([]history.Record{fresh}, p.Rows...)
	m, _ = m.Update(profileMsg{p: p})
	if got := m.prof.Rows[m.cursor].Excerpt; got != "Old finding." {
		t.Fatalf("the cursor must follow its record, now on %q", got)
	}
	p.Rows = p.Rows[:1]
	m, _ = m.Update(profileMsg{p: p})
	if m.cursor != 0 {
		t.Fatalf("a vanished record clamps the cursor: %d", m.cursor)
	}
}

func TestToggleAndNilHistory(t *testing.T) {
	m := openProfile(t, nil)
	if !strings.Contains(m.View(), "LAST 30 DAYS") {
		t.Fatal("30 days is the default")
	}
	m, _ = ppress(t, m, "tab")
	if !strings.Contains(m.View(), "LAST 7 DAYS") {
		t.Fatal("tab shows 7 days")
	}
	m, _ = ppress(t, m, "tab")
	if !strings.Contains(m.View(), "LAST 30 DAYS") {
		t.Fatal("tab again returns to 30 days")
	}
	n := New(Deps{Dot: view.LibraryDot{Slug: "security", Name: "Security"}})
	n, _ = pdrive(t, n, n.Init())
	for _, k := range []string{"j", "k", "enter", "tab", "x"} {
		n, _ = ppress(t, n, k)
	}
	_ = n.View()
}

func TestHintsHaveNoUnboundKey(t *testing.T) {
	s := history.Stats{Runs: map[string]int{"review": 10}, Errors: 5}
	hs := Hints(s, 0)
	if len(hs) != 1 || strings.Contains(hs[0], "press") {
		t.Fatalf("%v", hs)
	}
}

func TestSummarizeReportsPartialFailures(t *testing.T) {
	_, st := fixture(t)
	if _, err := st.Add(pPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "x", Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	st.SeeProject(memory.Project{ID: "g-fedcba9876543210", Name: "other", Root: "/other"})
	if _, err := st.Add("g-fedcba9876543210", memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "other note", Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	clean, err := Summarize(st, "security")
	if err != nil || clean.Partial || clean.Projects != 2 {
		t.Fatalf("clean: %+v %v", clean, err)
	}
	corrupted := 0
	filepath.Walk(filepath.Join(st.Dir, pPID), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.Contains(p, "security") {
			corrupted++
			return os.WriteFile(p, []byte("{not json"), 0o600)
		}
		return nil
	})
	if corrupted == 0 {
		t.Fatal("no scope file found to corrupt")
	}
	sum, _ := Summarize(st, "security")
	if !sum.Partial || sum.Entries == 0 {
		t.Fatalf("one project unreadable, the other still counted: %+v", sum)
	}
	m := New(Deps{Memory: st, Dot: view.LibraryDot{Slug: "security", Name: "Security"}})
	m, _ = m.Update(memMsg{sum: sum})
	if !strings.Contains(m.View(), "(partial)") {
		t.Fatalf("partial counts are labelled:\n%s", m.View())
	}
}

func TestCachedShare(t *testing.T) {
	oldCompute, oldClock := computeShare, shareClock
	defer func() { computeShare, shareClock = oldCompute, oldClock }()
	shareMu.Lock()
	shareCache = map[shareKey]shareEntry{}
	shareMu.Unlock()
	clock := pNow
	shareClock = func() time.Time { return clock }
	calls, fail := 0, false
	computeShare = func(*history.Store, string, time.Time) (float64, error) {
		calls++
		if fail {
			return 0, errors.New("boom")
		}
		return 0.25 * float64(calls), nil
	}
	hs := history.Open(t.TempDir())
	if v, err := cachedShare(hs, "security", pNow); err != nil || v != 0.25 || calls != 1 {
		t.Fatalf("miss: %v %v %d", v, err, calls)
	}
	clock = clock.Add(shareTTL - time.Second)
	if v, _ := cachedShare(hs, "security", pNow); v != 0.25 || calls != 1 {
		t.Fatalf("hit inside the TTL: %v %d", v, calls)
	}
	if _, _ = cachedShare(hs, "frontend", pNow); calls != 2 {
		t.Fatalf("another slug is its own key: %d", calls)
	}
	clock = clock.Add(2 * time.Second)
	if v, _ := cachedShare(hs, "security", pNow); v != 0.75 || calls != 3 {
		t.Fatalf("expired entry recomputes: %v %d", v, calls)
	}
	clock = clock.Add(2 * shareTTL)
	fail = true
	if _, err := cachedShare(hs, "security", pNow); err == nil || calls != 4 {
		t.Fatalf("error: %v %d", err, calls)
	}
	fail = false
	if v, err := cachedShare(hs, "security", pNow); err != nil || calls != 5 || v == 0 {
		t.Fatalf("an error is not cached: %v %v %d", v, err, calls)
	}
}
