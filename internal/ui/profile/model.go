// Package profile is the dot profile screen in the manage popup: 7- and
// 30-day stats from the dot's history, a memory summary, deterministic tuning
// hints and the recent history, with the full message of any row.
//
// Nothing in View or Update reads history or memory: the loads run in the
// tea.Cmds from Init, and the cost share is cached (see cachedShare).
package profile

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// Rows is how many recent runs the profile reads.
const Rows = 50

type Deps struct {
	History *history.Store
	Memory  *memory.Store // nil: memory off
	Dot     view.LibraryDot
	Purpose string
	// Changed: a project dot whose file changed since it was trusted. Purpose
	// is then the trusted copy (what runs); the profile says so and T asks
	// the parent for the trust preview of the file (TrustMsg).
	Changed  bool
	Now      func() time.Time                    // nil = time.Now
	CardBody func(cardKey string) (string, bool) // full text of a card still in this session's queue; may be nil
}

// Messages the profile sends to its parent.
type (
	BackMsg   struct{}
	ExportMsg struct{ Slug string }
	MemoryMsg struct{ Slug, Name string }
	TrustMsg  struct{ Dot view.LibraryDot } // review the changed file of this project dot (Deps.Changed)
)

// changedNote is what a changed project dot's profile says.
const changedNote = "the file changed since it was trusted; this is the trusted copy"

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

// MemSummary counts a dot's live memory across every project. Partial is set
// when some memory files could not be read, so the counts may be low.
type MemSummary struct {
	Projects, Entries, Pinned int
	ByKind                    map[memory.Kind]int
	Partial                   bool
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
	week       bool   // false: last 30 days (default); tab toggles
	cursor     int    // index into prof.Rows, always re-derived from curKey on a refresh
	curKey     string // rowKey of the selected row
	detail     bool
	off        int  // first body line shown in the detail
	endSeen    bool // the end of the detail body has been on screen
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
	at    time.Time // shareClock at the computation
}

var (
	shareMu    sync.Mutex
	shareCache = map[shareKey]shareEntry{}
	shareClock = time.Now
	// computeShare is the slow call; tests replace it.
	computeShare = func(hs *history.Store, slug string, now time.Time) (float64, error) {
		share, _, err := hs.CostShare(slug, now, 30*24*time.Hour)
		return share, err
	}
)

// cachedShare runs only inside a tea.Cmd, never from View or Update. Errors
// are not cached.
func cachedShare(hs *history.Store, slug string, now time.Time) (float64, error) {
	k := shareKey{hs, slug}
	shareMu.Lock()
	e, ok := shareCache[k]
	shareMu.Unlock()
	if ok && shareClock().Sub(e.at) < shareTTL {
		return e.share, nil
	}
	share, err := computeShare(hs, slug, now)
	if err == nil {
		shareMu.Lock()
		shareCache[k] = shareEntry{share, shareClock()}
		shareMu.Unlock()
	}
	return share, err
}

// Summarize counts slug's live dot and global memory over every project.
// Unreadable memory files do not fail the summary: the counts that could be
// read are returned with Partial set. An error comes back only when nothing
// could be listed.
func Summarize(st *memory.Store, slug string) (MemSummary, error) {
	sum := MemSummary{ByKind: map[memory.Kind]int{}}
	if !memory.ValidSlug(slug) {
		return sum, nil
	}
	ps, err := st.Projects()
	if err != nil {
		if len(ps) == 0 {
			return sum, err
		}
		sum.Partial = true
	}
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
		es, lerr := st.List(p.ID, memory.Filter{Scope: memory.ScopeDot, Dot: slug})
		if lerr != nil {
			sum.Partial = true
		}
		if len(es) > 0 {
			sum.Projects++
		}
		add(es)
	}
	g, lerr := st.List("", memory.Filter{Scope: memory.ScopeGlobal, Dot: slug})
	if lerr != nil {
		sum.Partial = true
	}
	add(g)
	return sum, nil
}

// Hints are the deterministic tuning hints (spec §6.2) for a 30-day window.
// share is this dot's part of every dot's cost in the same window. They are
// suggestions; nothing is changed automatically.
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
		out = append(out, "Fails often. Check the backend and its recent errors.")
	}
	return out
}

// rowKey identifies a history row across refreshes.
func rowKey(r history.Record) string {
	return fmt.Sprintf("%s|%d|%s", r.Card, r.At.UnixNano(), r.Kind)
}

// CanAct reports whether a follow-up action may be accepted: never while a
// detail is open and its end has not been on screen. (The profile has no such
// action today; later screens built on it must check this.)
func (m Model) CanAct() bool { return !m.detail || m.endSeen }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m = m.settle()
	case profileMsg:
		m.prof, m.err, m.loaded = msg.p, msg.err, true
		m.cursor = min(m.cursor, max(0, len(m.prof.Rows)-1))
		for i, r := range m.prof.Rows {
			if m.curKey != "" && rowKey(r) == m.curKey {
				m.cursor = i
				break
			}
		}
		if m.cursor < len(m.prof.Rows) {
			m.curKey = rowKey(m.prof.Rows[m.cursor])
		}
		m = m.settle()
	case shareMsg:
		m.share, m.shareReady = msg.share, msg.err == nil
	case memMsg:
		m.mem, m.memErr = msg.sum, msg.err
	case tea.KeyMsg:
		return m.keys(msg)
	}
	return m, nil
}

func (m Model) moveTo(i int) Model {
	m.cursor = max(0, min(i, len(m.prof.Rows)-1))
	if m.cursor < len(m.prof.Rows) {
		m.curKey = rowKey(m.prof.Rows[m.cursor])
	}
	return m
}

func (m Model) keys(k tea.KeyMsg) (Model, tea.Cmd) {
	m.status = ""
	if m.detail {
		_, body, _, page := m.detailLayout()
		switch k.String() {
		case "esc", "q", "enter":
			m.detail, m.off, m.endSeen = false, 0, false
			return m, nil
		case "j", "down":
			m.off++
		case "k", "up":
			m.off--
		case "pgdown", "ctrl+d", " ":
			m.off += page
		case "pgup", "ctrl+u":
			m.off -= page
		case "g", "home":
			m.off = 0
		case "G", "end":
			m.off = len(body)
		}
		return m.settle(), nil
	}
	switch k.String() {
	case "esc", "q":
		return m, func() tea.Msg { return BackMsg{} }
	case "tab":
		m.week = !m.week
	case "j", "down":
		m = m.moveTo(m.cursor + 1)
	case "k", "up":
		m = m.moveTo(m.cursor - 1)
	case "enter":
		if m.cursor < len(m.prof.Rows) {
			m.detail, m.off = true, 0
			m = m.settle()
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
	case "T":
		if m.d.Changed {
			d := m.d.Dot
			return m, func() tea.Msg { return TrustMsg{Dot: d} }
		}
	}
	return m, nil
}

// settle clamps the detail scroll offset and records whether the end of the
// body has been on screen.
func (m Model) settle() Model {
	if !m.detail {
		return m
	}
	_, body, avail, _ := m.detailLayout()
	maxOff := max(0, len(body)-avail)
	m.off = max(0, min(m.off, maxOff))
	if m.off >= maxOff {
		m.endSeen = true
	}
	return m
}

func (m Model) w() int {
	if m.width > 0 {
		return max(m.width, 20)
	}
	return 80
}

func (m Model) h() int {
	if m.height > 0 {
		return m.height
	}
	return 0
}

func sep() string { return " " + style.Sym("·") + " " }

func pct(r float64) string { return fmt.Sprintf("%.0f%%", r*100) }

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

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
		parts[i] = fmt.Sprintf("%s %d", style.Clean(x.k), x.n)
	}
	line := fmt.Sprintf("runs %d", st.TotalRuns())
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, sep()) + ")"
	}
	return line + sep() + "flagged " + pct(st.FlaggedRate())
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
	line := fmt.Sprintf("%d %s%s%d entries", s.Projects, plural("project", s.Projects), sep(), s.Entries)
	if len(kinds) > 0 {
		line += " (" + strings.Join(kinds, sep()) + ")"
	}
	line += fmt.Sprintf("%s%d pinned", sep(), s.Pinned)
	if s.Partial {
		line += " (partial)"
	}
	return line
}

// section is a dim rule with a title, exactly w cells wide at most.
func section(title string, w int) string {
	dash := style.Sym("─")
	lead := dash + dash + " " + title + " "
	return style.Dim.Render(ansi.Truncate(lead+strings.Repeat(dash, max(0, w-ansi.StringWidth(lead))), w, ""))
}

// wrapped returns the lines of s wrapped to w cells, each rendered with f
// (nil: as is).
func wrapped(s string, w int, f func(string) string) []string {
	ls := style.Wrap(s, w)
	if f != nil {
		for i, l := range ls {
			ls[i] = f(l)
		}
	}
	return ls
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
	return style.Cut(line, m.w()-2)
}

// fit cuts every line of out to the width, by display cells.
func (m Model) fit(lines []string) string {
	w := m.w()
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, style.Sym("…"))
	}
	return strings.Join(lines, "\n")
}

func (m Model) heading() []string {
	d := m.d.Dot
	w := m.w()
	origin := style.Clean(d.Origin)
	if origin == "" {
		origin = dots.OriginUser
	}
	title := fmt.Sprintf("%s %s%s%s/%s%s%s", style.Glyph(style.Clean(d.Glyph), style.Clean(d.Color)), strings.ToUpper(style.Cut(style.Clean(d.Name), 40)), sep(),
		style.Clean(d.Backend), style.Clean(d.Model), sep(), style.Clean(d.Mode))
	return []string{
		style.Title.Render(ansi.Truncate(title, w, style.Sym("…"))),
		style.Dim.Render(style.Cut("origin: "+origin+sep()+style.Clean(dots.DisplaySlug(d.Slug)), w)),
	}
}

// bodyOf is the text of the selected row and whether it is the whole card.
func (m Model) bodyOf(r history.Record) (string, bool) {
	if m.d.CardBody != nil && r.Card != "" {
		if body, ok := m.d.CardBody(r.Card); ok {
			return body, true
		}
	}
	return r.Excerpt, false
}

// detailLayout lays out the open detail: the fixed top lines, the whole
// scrollable body, how many body lines fit, and the page size.
func (m Model) detailLayout() (top, body []string, avail, page int) {
	if m.cursor >= len(m.prof.Rows) {
		return nil, nil, 1, 1
	}
	w := m.w()
	r := m.prof.Rows[m.cursor]
	text, full := m.bodyOf(r)
	top = append(top, m.heading()...)
	top = append(top, section("RUN "+r.At.Local().Format("2006-01-02 15:04:05"), w))
	top = append(top, wrapped(fmt.Sprintf("kind %s%soutcome %s%sseverity %s%sresolution %s%sturn %d",
		style.Clean(string(r.Kind)), sep(), style.Clean(string(r.Outcome)), sep(), style.Clean(r.Severity), sep(), style.Clean(string(r.Resolution)), sep(), r.Turn), w-2, nil)...)
	body = style.Wrap(text, w-2)
	if !full && r.Excerpt != "" {
		body = append(body, wrapped("(history keeps the first 300 characters; the card is no longer in the queue)", w-2, dim)...)
	}
	h := m.h()
	if h == 0 {
		h = 24
	}
	foot := 2
	avail = max(3, h-len(top)-foot)
	if len(body) > avail {
		avail-- // the "more below" marker takes a line
	}
	return top, body, avail, max(1, avail-1)
}

func (m Model) detailView() string {
	top, body, avail, _ := m.detailLayout()
	off := max(0, min(m.off, max(0, len(body)-avail)))
	lines := append([]string{}, top...)
	end := min(len(body), off+avail)
	lines = append(lines, body[off:end]...)
	if end < len(body) {
		lines = append(lines, style.Dim.Render(fmt.Sprintf("%s more below (%d lines)", style.Sym("▼"), len(body)-end)))
	}
	lines = append(lines, "", style.Dim.Render("j/k pgup/pgdn scroll"+sep()+"esc back"))
	return m.fit(lines)
}

// parts builds the lines above and below the history rows. compact drops the
// purpose, memory, hints and spacing when the popup is short.
func (m Model) parts(compact bool) (head, tail []string) {
	w := m.w()
	head = m.heading()
	if m.d.Changed { // even when compact: what runs isn't what the file says
		head = append(head, wrapped(changedNote, w-2, errStyle)...)
	}
	if p := style.Wrap(m.d.Purpose, w-10); strings.TrimSpace(m.d.Purpose) != "" && !compact {
		line := "Purpose: " + p[0]
		if len(p) > 1 {
			line += style.Sym("…")
		}
		head = append(head, line)
	}
	title, st := "LAST 30 DAYS", m.prof.Month
	if m.week {
		title, st = "LAST 7 DAYS", m.prof.Week
	}
	head = append(head, section(title, w))
	switch {
	case !m.loaded:
		head = append(head, style.Dim.Render("loading"+style.Sym("…")))
	case m.err != nil:
		head = append(head, wrapped("history unavailable: "+style.Clean(m.err.Error()), w-2, errStyle)...)
	default:
		head = append(head, wrapped(runsLine(st), w-2, nil)...)
		cost := fmt.Sprintf("cost $%.2f", st.CostUSD)
		if st.CostUnknown > 0 {
			cost += fmt.Sprintf(" (+%d unpriced %s)", st.CostUnknown, plural("run", st.CostUnknown))
		}
		head = append(head, wrapped(fmt.Sprintf("accepted %s (%d / %d)%ssuppressed %d%s%s%smedian %.1f s",
			pct(st.AcceptanceRate()), st.Accepted, st.Resolved, sep(), st.Suppressed, sep(), cost, sep(), float64(st.MedianMs)/1000), w-2, nil)...)
	}
	if !compact {
		head = append(head, section("MEMORY", w))
		head = append(head, wrapped(m.memLine(), w-2, nil)...)
		if m.loaded && m.err == nil {
			share := 0.0
			if m.shareReady {
				share = m.share
			}
			if hs := Hints(m.prof.Month, share); len(hs) > 0 {
				head = append(head, section("HINTS", w))
				for _, h := range hs {
					head = append(head, wrapped(h, w-2, nil)...)
				}
			}
		}
	}
	head = append(head, section("HISTORY", w))
	if m.loaded && m.err == nil && len(m.prof.Rows) == 0 {
		head = append(head, style.Dim.Render("no runs yet"))
	}
	if m.loaded && m.prof.Month.Skipped > 0 {
		tail = append(tail, style.Dim.Render(fmt.Sprintf("(%d unreadable history lines skipped)", m.prof.Month.Skipped)))
	}
	if m.status != "" {
		tail = append(tail, wrapped(m.status, w-2, nil)...)
	}
	if !compact {
		tail = append(tail, "")
	}
	keys := "enter details" + sep() + "M memory" + sep() + "x export" + sep() + "tab 7d/30d" + sep() + "esc back"
	if m.d.Changed {
		keys = "T review the file" + sep() + keys
	}
	tail = append(tail, wrapped(keys, w-2, dim)...)
	return head, tail
}

func (m Model) View() string {
	if m.detail && m.cursor < len(m.prof.Rows) {
		return m.detailView()
	}
	head, tail := m.parts(false)
	avail := 10
	if h := m.h(); h > 0 {
		avail = h - len(head) - len(tail)
		if avail < 3 {
			head, tail = m.parts(true)
			avail = h - len(head) - len(tail)
		}
	}
	avail = max(1, avail)
	n := len(m.prof.Rows)
	start := max(0, min(m.cursor-avail/2, n-avail))
	lines := head
	for i := start; i < min(n, start+avail); i++ {
		marker := "  "
		if i == m.cursor {
			marker = style.Sym("▸") + " "
		}
		line := marker + m.row(m.prof.Rows[i])
		if i == m.cursor {
			line = style.Selected.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, tail...)
	return m.fit(lines)
}

func dim(s string) string      { return style.Dim.Render(s) }
func errStyle(s string) string { return style.Err.Render(s) }
