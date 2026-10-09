// Package profile is the dot profile screen in the manage popup: 7- and
// 30-day stats from the dot's history, a memory summary, deterministic tuning
// hints and the recent history, with the full message of any row.
package profile

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// Rows is how many recent runs the profile reads.
const Rows = 50

type Deps struct {
	History  *history.Store
	Memory   *memory.Store // nil: memory off
	Dot      view.LibraryDot
	Purpose  string
	Now      func() time.Time                    // nil = time.Now
	CardBody func(cardKey string) (string, bool) // full text of a card still in this session's queue; may be nil
}

// Messages the profile sends to its parent.
type (
	BackMsg   struct{}
	ExportMsg struct{ Slug string }
	MemoryMsg struct{ Slug, Name string }
)

type (
	profileMsg struct {
		p   history.Profile
		err error
	}
	shareMsg struct {
		share float64
		err   error
	}
	memMsg struct {
		sum MemSummary
		err error
	}
)

// MemSummary counts a dot's live memory across every project.
type MemSummary struct {
	Projects, Entries, Pinned int
	ByKind                    map[memory.Kind]int
}

type Model struct {
	d          Deps
	prof       history.Profile
	loaded     bool
	err        error
	share      float64
	shareReady bool
	mem        MemSummary
	memErr     error
	week       bool // false: last 30 days (default); tab toggles
	cursor     int
	detail     bool
	status     string
	width      int
	height     int
}

func New(d Deps) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	return Model{d: d}
}

// Init loads the profile, the cost share (it reads every dot's history, so
// it comes second) and the memory summary off the UI goroutine.
func (m Model) Init() tea.Cmd {
	hs, slug, now, st := m.d.History, m.d.Dot.Slug, m.d.Now(), m.d.Memory
	return tea.Batch(
		func() tea.Msg {
			if hs == nil {
				return profileMsg{err: fmt.Errorf("history is not available")}
			}
			p, err := hs.Profile(slug, now, Rows)
			return profileMsg{p: p, err: err}
		},
		func() tea.Msg {
			if hs == nil {
				return shareMsg{}
			}
			share, err := cachedShare(hs, slug, now)
			return shareMsg{share: share, err: err}
		},
		func() tea.Msg {
			if st == nil {
				return memMsg{}
			}
			sum, err := Summarize(st, slug)
			return memMsg{sum: sum, err: err}
		},
	)
}

// shareTTL bounds how stale a cached cost share may be: CostShare reads every
// dot's history (about 330 ms), so reopening a profile reuses a recent value.
const shareTTL = 60 * time.Second

type shareKey struct {
	hs   *history.Store
	slug string
}

type shareEntry struct {
	share float64
	at    time.Time // wall clock of the computation
}

var (
	shareMu    sync.Mutex
	shareCache = map[shareKey]shareEntry{}
)

// cachedShare runs only inside a tea.Cmd, never from View or Update.
func cachedShare(hs *history.Store, slug string, now time.Time) (float64, error) {
	k := shareKey{hs, slug}
	shareMu.Lock()
	e, ok := shareCache[k]
	shareMu.Unlock()
	if ok && time.Since(e.at) < shareTTL {
		return e.share, nil
	}
	share, _, err := hs.CostShare(slug, now, 30*24*time.Hour)
	if err == nil {
		shareMu.Lock()
		shareCache[k] = shareEntry{share, time.Now()}
		shareMu.Unlock()
	}
	return share, err
}

// Summarize counts slug's live dot and global memory over every project.
func Summarize(st *memory.Store, slug string) (MemSummary, error) {
	sum := MemSummary{ByKind: map[memory.Kind]int{}}
	if !memory.ValidSlug(slug) {
		return sum, nil
	}
	ps, err := st.Projects()
	add := func(es []memory.Entry) {
		for _, e := range es {
			sum.Entries++
			sum.ByKind[e.Kind]++
			if e.Pinned {
				sum.Pinned++
			}
		}
	}
	for _, p := range ps {
		es, _ := st.List(p.ID, memory.Filter{Scope: memory.ScopeDot, Dot: slug})
		if len(es) > 0 {
			sum.Projects++
		}
		add(es)
	}
	g, _ := st.List("", memory.Filter{Scope: memory.ScopeGlobal, Dot: slug})
	add(g)
	return sum, err
}

// Hints are the deterministic tuning hints (spec §6.2) for a 30-day window.
// share is this dot's part of every dot's cost in the same window.
func Hints(s history.Stats, share float64) []string {
	var out []string
	if s.Resolved >= 10 && s.AcceptanceRate() < 0.20 {
		out = append(out, "Mostly dismissed. Tighten the purpose, raise cooldown, or switch to suggest.")
	}
	if s.Verdicts >= 50 && s.FlaggedRate() < 0.03 {
		out = append(out, "Rarely speaks. Consider observe mode or narrower file_globs.")
	}
	if share >= 0.5 && s.CostUSD >= 1 {
		out = append(out, "Your most expensive dot. Try a cheaper model or lower effort.")
	}
	if s.Suppressed >= 5 {
		out = append(out, "Keeps repeating dismissed advice. Review its dismissed memories.")
	}
	if s.TotalRuns() >= 10 && s.ErrorRate() >= 0.20 {
		out = append(out, "Fails often. Check the backend (press r to reset).")
	}
	return out
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case profileMsg:
		m.prof, m.err, m.loaded = msg.p, msg.err, true
		m.cursor = min(m.cursor, max(0, len(m.prof.Rows)-1))
	case shareMsg:
		m.share, m.shareReady = msg.share, msg.err == nil
	case memMsg:
		m.mem, m.memErr = msg.sum, msg.err
	case tea.KeyMsg:
		return m.keys(msg)
	}
	return m, nil
}

func (m Model) keys(k tea.KeyMsg) (Model, tea.Cmd) {
	m.status = ""
	if m.detail {
		switch k.String() {
		case "esc", "q", "enter":
			m.detail = false
		}
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		return m, func() tea.Msg { return BackMsg{} }
	case "tab":
		m.week = !m.week
	case "j", "down":
		m.cursor = min(m.cursor+1, max(0, len(m.prof.Rows)-1))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "enter":
		if m.cursor < len(m.prof.Rows) {
			m.detail = true
		}
	case "x":
		if m.d.Dot.Origin == dots.OriginProject {
			m.status = "project dots are shared through their repository"
			return m, nil
		}
		slug := m.d.Dot.Slug
		return m, func() tea.Msg { return ExportMsg{Slug: slug} }
	case "M":
		if m.d.Memory == nil {
			m.status = "memory is turned off (memory_enabled = false)"
			return m, nil
		}
		slug, name := m.d.Dot.Slug, m.d.Dot.Name
		return m, func() tea.Msg { return MemoryMsg{Slug: slug, Name: name} }
	}
	return m, nil
}

func (m Model) w() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

// cut shortens s to at most n runes.
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:max(1, n-1)]) + style.Sym("…")
	}
	return s
}

func pct(r float64) string { return fmt.Sprintf("%.0f%%", r*100) }

func runsLine(st history.Stats) string {
	type kv struct {
		k string
		n int
	}
	var kinds []kv
	for k, n := range st.Runs {
		kinds = append(kinds, kv{k, n})
	}
	sort.Slice(kinds, func(i, j int) bool {
		if kinds[i].n != kinds[j].n {
			return kinds[i].n > kinds[j].n
		}
		return kinds[i].k < kinds[j].k
	})
	parts := make([]string, len(kinds))
	for i, x := range kinds {
		parts[i] = fmt.Sprintf("%s %d", x.k, x.n)
	}
	line := fmt.Sprintf("runs %d", st.TotalRuns())
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, " · ") + ")"
	}
	return line + "   flagged " + pct(st.FlaggedRate())
}

func (m Model) memLine() string {
	if m.d.Memory == nil {
		return "memory is turned off"
	}
	if m.memErr != nil {
		return "memory unavailable: " + style.Clean(m.memErr.Error())
	}
	s := m.mem
	var kinds []string
	for _, k := range []memory.Kind{memory.KindFinding, memory.KindDismissed, memory.KindConvention, memory.KindDecision, memory.KindNote, memory.KindPreference, memory.KindResearch, memory.KindQuestion} {
		if n := s.ByKind[k]; n > 0 {
			kinds = append(kinds, fmt.Sprintf("%d %s", n, plural(string(k), n)))
		}
	}
	line := fmt.Sprintf("%d %s · %d entries", s.Projects, plural("project", s.Projects), s.Entries)
	if len(kinds) > 0 {
		line += " (" + strings.Join(kinds, " · ") + ")"
	}
	return line + fmt.Sprintf(" · %d pinned", s.Pinned)
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func section(title string, w int) string {
	return style.Dim.Render("── " + title + " " + strings.Repeat("─", max(0, w-len(title)-5)))
}

func (m Model) row(r history.Record) string {
	res := r.Resolution
	if res == "" && r.Card != "" {
		res = "pending"
	}
	sev := r.Severity
	if sev == "" {
		sev = r.Outcome
	}
	line := fmt.Sprintf("%s  %-9s %-9s %-10s %s", r.At.Local().Format("01-02 15:04"), style.Clean(string(r.Kind)), style.Clean(string(sev)), style.Clean(string(res)), style.Clean(r.Excerpt))
	if rs := []rune(line); len(rs) > m.w()-2 {
		line = string(rs[:max(10, m.w()-3)]) + style.Sym("…") // Phase 6: ASCII stand-in with HERDR_DOTS_ASCII=1
	}
	return line
}

func (m Model) View() string {
	w := m.w()
	d := m.d.Dot
	var b strings.Builder
	origin := d.Origin
	if origin == "" {
		origin = dots.OriginUser
	}
	title := fmt.Sprintf("%s %s · %s/%s · %s", style.Glyph(style.Clean(d.Glyph), style.Clean(d.Color)), strings.ToUpper(cut(style.Clean(d.Name), 40)), style.Clean(d.Backend), style.Clean(d.Model), style.Clean(d.Mode))
	b.WriteString(style.Title.Render(title))
	b.WriteString(style.Dim.Render("   origin: "+style.Clean(origin)+"   "+style.Clean(dots.DisplaySlug(d.Slug))) + "\n")
	if m.detail && m.cursor < len(m.prof.Rows) {
		r := m.prof.Rows[m.cursor]
		text, full := r.Excerpt, false
		if m.d.CardBody != nil && r.Card != "" {
			if body, ok := m.d.CardBody(r.Card); ok {
				text, full = body, true
			}
		}
		b.WriteString(section("RUN "+r.At.Local().Format("2006-01-02 15:04:05"), w) + "\n")
		b.WriteString(fmt.Sprintf("kind %s · outcome %s · severity %s · resolution %s · turn %d\n", style.Clean(string(r.Kind)), style.Clean(string(r.Outcome)), style.Clean(r.Severity), style.Clean(string(r.Resolution)), r.Turn))
		for _, l := range style.Wrap(text, w-2) {
			b.WriteString(l + "\n")
		}
		if !full && r.Excerpt != "" {
			b.WriteString(style.Dim.Render("(history keeps the first 300 characters; the card is no longer in the queue)") + "\n")
		}
		b.WriteString("\n" + style.Dim.Render("esc back"))
		return b.String()
	}
	if p := style.Wrap(m.d.Purpose, w-11); len(p) > 0 {
		line := "Purpose: " + p[0]
		if len(p) > 1 {
			line += style.Sym("…")
		}
		b.WriteString(line + "\n")
	}
	title, st := "LAST 30 DAYS", m.prof.Month
	if m.week {
		title, st = "LAST 7 DAYS", m.prof.Week
	}
	b.WriteString(section(title, w) + "\n")
	switch {
	case !m.loaded:
		b.WriteString(style.Dim.Render("loading"+style.Sym("…")) + "\n")
	case m.err != nil:
		b.WriteString(style.Err.Render("history unavailable: "+style.Clean(m.err.Error())) + "\n")
	default:
		b.WriteString(runsLine(st) + "\n")
		cost := fmt.Sprintf("cost $%.2f", st.CostUSD)
		if st.CostUnknown > 0 {
			cost += fmt.Sprintf(" (+%d unpriced %s)", st.CostUnknown, plural("run", st.CostUnknown))
		}
		b.WriteString(fmt.Sprintf("accepted %s (%d / %d)   suppressed %d   %s   median %.1f s\n",
			pct(st.AcceptanceRate()), st.Accepted, st.Resolved, st.Suppressed, cost, float64(st.MedianMs)/1000))
	}
	b.WriteString(section("MEMORY", w) + "\n" + strings.Join(style.Wrap(m.memLine(), w-2), "\n") + "\n")
	if m.loaded && m.err == nil {
		share := 0.0
		if m.shareReady {
			share = m.share
		}
		if hs := Hints(m.prof.Month, share); len(hs) > 0 {
			b.WriteString(section("HINTS", w) + "\n")
			for _, h := range hs {
				b.WriteString(strings.Join(style.Wrap(h, w-2), "\n") + "\n")
			}
		}
	}
	b.WriteString(section("HISTORY", w) + "\n")
	if m.loaded && len(m.prof.Rows) == 0 {
		b.WriteString(style.Dim.Render("no runs yet") + "\n")
	}
	n := max(5, m.height-14)
	if m.height == 0 {
		n = 10
	}
	start := max(0, min(m.cursor-n/2, len(m.prof.Rows)-n))
	for i := start; i < min(len(m.prof.Rows), start+n); i++ {
		marker := "  "
		if i == m.cursor {
			marker = style.Sym("▸") + " "
		}
		line := marker + m.row(m.prof.Rows[i])
		if i == m.cursor {
			line = style.Selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if m.loaded && m.prof.Month.Skipped > 0 {
		b.WriteString(style.Dim.Render(fmt.Sprintf("(%d unreadable history lines skipped)", m.prof.Month.Skipped)) + "\n")
	}
	if m.status != "" {
		b.WriteString(m.status + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("enter details · M memory · x export · tab 7d/30d · esc back"))
	return b.String()
}
