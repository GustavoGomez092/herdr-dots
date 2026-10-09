// Package pane is the Dots side pane: roster, approval queue and feed for one
// main agent pane.
package pane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

type Caller interface {
	Call(ctx context.Context, cmd string, args any) error
}

type SnapshotMsg view.Snapshot
type DisconnectedMsg struct{}
type errMsg struct{ err error }

type area int

const (
	areaQueue area = iota
	areaRoster
)

type Model struct {
	target    string
	caller    Caller
	snap      view.Snapshot
	connected bool
	area      area
	roster    int
	card      int
	dotSlug   string // selected roster dot, re-found by slug on every snapshot
	cardID    string // selected card, re-found by id on every snapshot
	showFeed  bool
	showHelp  bool
	editing   bool
	editID    string
	editor    textarea.Model
	asking    bool
	askSlug   string
	question  textinput.Model
	// M chooser: remember the selected card (f/r/c/g); r asks for a reason.
	// A council card offers c only (rememberCouncil).
	remembering     bool
	rememberID      string
	rememberCouncil bool
	reasoning       bool
	reason          textinput.Model
	width           int
	height          int // 0 = unknown: render everything
	status          string
	seen            bool // a snapshot or disconnect has been received

	// Council (council.go).
	expandID string                    // council card whose member answers are shown
	opener   func(target string) error // opens the council popup (WithOpener); nil = unavailable
}

const offlineHint = "offline: actions unavailable until the engine restarts"

const engineDownMsg = "engine down, restarting…"

func New(target string, c Caller) Model {
	ta := textarea.New()
	ta.SetHeight(6)
	ta.ShowLineNumbers = false
	ti := textinput.New()
	ti.Placeholder = "optional question, enter to summon"
	ri := textinput.New()
	ri.Placeholder = "optional reason, enter to save"
	return Model{target: target, caller: c, editor: ta, question: ti, reason: ri}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) pane() view.Pane {
	p, _ := m.snap.Pane(m.target)
	return p
}

func (m Model) selectedCard() (queue.Card, bool) {
	p := m.pane()
	if m.card >= 0 && m.card < len(p.Cards) {
		return p.Cards[m.card], true
	}
	return queue.Card{}, false
}

func (m Model) selectedDot() (view.DotState, bool) {
	p := m.pane()
	if m.roster >= 0 && m.roster < len(p.Dots) {
		return p.Dots[m.roster], true
	}
	return view.DotState{}, false
}

func sendable(c queue.Card) bool {
	return (c.Status == queue.StatusPending || c.Status == queue.StatusAuto) && c.Mode != string(dots.ModeSuggest)
}

// clamp keeps the indexes in range and records which card and dot they point
// at, so the selection can follow them when a snapshot reorders the lists.
func (m *Model) clamp() {
	p := m.pane()
	m.card = max(0, min(m.card, len(p.Cards)-1))
	m.roster = max(0, min(m.roster, len(p.Dots)-1))
	m.cardID, m.dotSlug = "", ""
	if m.card < len(p.Cards) {
		m.cardID = p.Cards[m.card].ID
	}
	if m.roster < len(p.Dots) {
		m.dotSlug = p.Dots[m.roster].Slug
	}
}

// reselect moves the indexes to the previously selected card and dot in a new
// snapshot (a new higher-severity card shifts indexes). If one is gone, the
// index stays put and selects its neighbour.
func (m *Model) reselect() {
	p := m.pane()
	for i, c := range p.Cards {
		if m.cardID != "" && c.ID == m.cardID {
			m.card = i
			break
		}
	}
	for i, d := range p.Dots {
		if m.dotSlug != "" && d.Slug == m.dotSlug {
			m.roster = i
			break
		}
	}
	m.clamp()
}

func (m *Model) move(d int) {
	if m.area == areaRoster {
		m.roster += d
	} else {
		m.card += d
	}
	m.clamp()
}

func nextTarget(s view.Snapshot, cur string) string {
	for i, p := range s.Panes {
		if p.PaneID == cur && len(s.Panes) > 0 {
			return s.Panes[(i+1)%len(s.Panes)].PaneID
		}
	}
	if len(s.Panes) > 0 {
		return s.Panes[0].PaneID
	}
	return cur
}

func (m Model) call(cmd string, args any) tea.Cmd {
	c := m.caller
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Call(ctx, cmd, args); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.editor.SetWidth(max(20, msg.Width-2))
		return m, nil
	case SnapshotMsg:
		m.snap, m.connected, m.seen = view.Snapshot(msg), true, true
		if m.status == offlineHint {
			m.status = ""
		}
		m.reselect()
		return m, nil
	case DisconnectedMsg:
		m.connected, m.seen = false, true
		return m, nil
	case errMsg:
		m.status = msg.err.Error()
		return m, nil
	case tea.KeyMsg:
		switch {
		case m.editing:
			return m.updateEditor(msg)
		case m.asking:
			return m.updateQuestion(msg)
		case m.remembering:
			return m.updateRemember(msg)
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m Model) updateEditor(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.editing = false
		return m, nil
	case "ctrl+s":
		m.editing = false
		return m, m.call(ipc.CmdEdit, ipc.CardArgs{ID: m.editID, Body: m.editor.Value()})
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(k)
	return m, cmd
}

func (m Model) updateQuestion(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.asking = false
		return m, nil
	case "enter":
		m.asking = false
		m.status = "summoned " + m.askSlug
		return m, m.call(ipc.CmdSummon, ipc.SummonArgs{PaneID: m.target, Slug: m.askSlug, Question: strings.TrimSpace(m.question.Value())})
	}
	var cmd tea.Cmd
	m.question, cmd = m.question.Update(k)
	return m, cmd
}

// updateRemember handles the M chooser: f accepted finding, r rule (asks for
// an optional reason), c project convention, g this dot everywhere.
func (m Model) updateRemember(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.reasoning {
		switch k.String() {
		case "esc":
			m.remembering, m.reasoning = false, false
			return m, nil
		case "enter":
			m.remembering, m.reasoning = false, false
			m.status = "remembered as a rule"
			return m, m.call(ipc.CmdRemember, ipc.RememberArgs{CardID: m.rememberID, As: "rule", Reason: strings.TrimSpace(m.reason.Value())})
		}
		var cmd tea.Cmd
		m.reason, cmd = m.reason.Update(k)
		return m, cmd
	}
	var as string
	key := k.String()
	if m.rememberCouncil && key != "esc" && key != "c" {
		return m, nil // a council card is remembered as a project convention only
	}
	switch key {
	case "esc":
		m.remembering = false
		return m, nil
	case "f":
		as = "finding"
	case "r":
		m.reasoning = true
		m.reason.SetValue("")
		m.reason.Focus()
		return m, textinput.Blink
	case "c":
		as = "convention"
	case "g":
		as = "global"
	default:
		return m, nil
	}
	m.remembering = false
	m.status = "remembered as " + as
	return m, m.call(ipc.CmdRemember, ipc.RememberArgs{CardID: m.rememberID, As: as})
}

func (m Model) updateKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	if mm, cmd, ok := m.councilKeys(k); ok {
		return mm, cmd
	}
	switch k.String() {
	case "enter", "a", "x", "e", "s", "m", "p", "d", "r", "M":
		if !m.connected {
			m.status = offlineHint
			return m, nil
		}
	}
	switch k.String() {
	case "enter", "x", "e", "M":
		if m.area != areaQueue {
			m.status = "tab to the queue to act on a card"
			return m, nil
		}
	case "s", "m", "p", "d", "r":
		if m.area != areaRoster {
			m.status = "tab to the roster to act on a dot"
			return m, nil
		}
	}
	switch k.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
	case "f":
		m.showFeed = !m.showFeed
	case "tab":
		if m.area == areaQueue {
			m.area = areaRoster
		} else {
			m.area = areaQueue
		}
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "t":
		m.target = nextTarget(m.snap, m.target)
		m.roster, m.card = 0, 0
		m.clamp()
	case "enter":
		c, ok := m.selectedCard()
		if !ok {
			return m, nil
		}
		if c.Mode == string(dots.ModeSuggest) {
			m.status = "suggestions can't be sent: copy the text instead"
			return m, nil
		}
		if sendable(c) {
			return m, m.call(ipc.CmdSend, ipc.SendArgs{IDs: []string{c.ID}})
		}
	case "a":
		var ids []string
		for _, c := range m.pane().Cards {
			if sendable(c) {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) > 0 {
			return m, m.call(ipc.CmdSend, ipc.SendArgs{IDs: ids})
		}
	case "x":
		if c, ok := m.selectedCard(); ok {
			return m, m.call(ipc.CmdDismiss, ipc.CardArgs{ID: c.ID})
		}
	case "M":
		if c, ok := m.selectedCard(); ok {
			m.remembering, m.reasoning, m.rememberID = true, false, c.ID
			m.rememberCouncil = c.Kind == queue.KindCouncil
		}
	case "e":
		if c, ok := m.selectedCard(); ok && sendable(c) {
			m.editing, m.editID = true, c.ID
			m.editor.SetValue(c.Body)
			m.editor.CursorEnd()
			m.editor.Focus()
			return m, textarea.Blink
		}
	case "s":
		if d, ok := m.selectedDot(); ok {
			m.asking, m.askSlug = true, d.Slug
			m.question.SetValue("")
			m.question.Focus()
			return m, textinput.Blink
		}
	case "m":
		if d, ok := m.selectedDot(); ok {
			return m, m.call(ipc.CmdSetMode, ipc.SetModeArgs{Slug: d.Slug, Mode: string(dots.Mode(d.Mode).Next())})
		}
	case "p":
		if d, ok := m.selectedDot(); ok {
			return m, m.call(ipc.CmdPause, ipc.PauseArgs{PaneID: m.target, Slug: d.Slug, Paused: !d.Paused})
		}
	case "d":
		if d, ok := m.selectedDot(); ok {
			return m, m.call(ipc.CmdDetach, ipc.DotArgs{PaneID: m.target, Slug: d.Slug})
		}
	case "r":
		if d, ok := m.selectedDot(); ok {
			return m, m.call(ipc.CmdReset, ipc.DotArgs{PaneID: m.target, Slug: d.Slug})
		}
	}
	return m, nil
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func rule(title string, w int) string {
	return style.Dim.Render("── "+title+" "+strings.Repeat("─", max(0, w-len(title)-4))) + "\n"
}

const helpText = `j/k move   tab roster/queue   t next agent
⏎ send card   a send all   e edit   x dismiss
M remember card (finding, rule, convention, global)
s summon dot   m cycle mode   p pause   d detach
r reset dot+budget    f feed   ? help   q close`

// feedOutcome shows memory outcomes with their detail: "learned 2",
// "suppressed (d:m17)", and agent asks as "◆ asked by agent · <severity>"
// (the cost tail of an ask is never shown).
func feedOutcome(e state.FeedEntry) string {
	switch e.Outcome {
	case state.OutcomeLearned:
		if e.Detail != "" {
			return e.Detail
		}
	case state.OutcomeSuppressed:
		return "suppressed (" + strings.TrimPrefix(e.Detail, "similar to ") + ")"
	case state.OutcomeAsked:
		sev, _, _ := strings.Cut(e.Detail, " · ")
		return "◆ asked by agent · " + sev
	}
	return e.Outcome
}

func (m Model) View() string {
	w := m.width
	if w <= 0 {
		w = 46
	}
	var b strings.Builder
	p, ok := m.snap.Pane(m.target)
	agent := p.Agent
	if agent == "" {
		agent = "agent"
	}
	lead := fmt.Sprintf("DOTS → %s (%s)", agent, m.target)
	project, tail := "", ""
	if ok && p.ProjectName != "" {
		project = fmt.Sprintf(" · %s · mem %d", p.ProjectName, p.MemoryEntries)
		if p.MemoryProposals > 0 {
			project += fmt.Sprintf(" (%d proposals)", p.MemoryProposals)
		}
	}
	if ok {
		tail = fmt.Sprintf("  budget %d/%d", p.Calls, p.Budget) + paneCost(p.CostUSD, p.BudgetUSD, p.CostUnknown)
	}
	if m.snap.PausedAll {
		tail += "  ⏸ all paused"
	}
	// The project segment gives way first, so the budget and cost tail
	// survives a narrow pane; one final cut keeps the header on one line.
	if room := w - len([]rune(lead)) - len([]rune(tail)); room < len([]rune(project)) {
		if room <= 0 {
			project = ""
		} else {
			project = trunc(project, room)
		}
	}
	b.WriteString(style.Title.Render(trunc(lead+project+tail, max(10, w))) + "\n")
	if p.Source == "scrollback" {
		b.WriteString(style.Dim.Render("(reading scrollback: transcript unavailable)") + "\n")
	}
	if m.seen && !m.connected {
		b.WriteString(style.Err.Render(engineDownMsg) + "\n")
	}
	if len(p.Dots) == 0 {
		b.WriteString(style.Dim.Render("No dots here yet. Press prefix+d to add some.") + "\n")
	}
	top := b.String() // title and banners: always on screen
	roster := make([]string, 0, len(p.Dots))
	for i, d := range p.Dots {
		status := d.Status
		if d.Learning {
			status = style.Sym("◌") + " learning" + style.Sym("…")
		}
		rest := fmt.Sprintf("%-12s %-8s %s", trunc(d.Name, 12), d.Mode, status) + dotCost(d)
		line := style.Glyph(d.Glyph, d.Color) + " " + trunc(rest, max(10, w-2))
		if m.area == areaRoster && i == m.roster {
			line = style.Selected.Render(line)
		}
		roster = append(roster, line+"\n")
	}
	queueRule := rule(fmt.Sprintf("QUEUE (%d)", len(p.Cards)), w)
	if p.Council != nil {
		// The progress line rides with the queue rule, so the roster cut
		// (fitRoster) counts it and it never scrolls away.
		queueRule = style.Dim.Render(trunc(councilProgress(*p.Council), max(10, w))) + "\n" + queueRule
	}

	// Body: cards, feed and help, scrolled so the selected card stays visible.
	b.Reset()
	selStart, selEnd := -1, -1
	body := lipgloss.NewStyle().Width(max(10, w-2))
	for i, c := range p.Cards {
		if i == m.card {
			selStart = lineCount(b.String())
		}
		sel := m.area == areaQueue && i == m.card
		marker := " "
		if sel {
			marker = "▸"
		}
		tags := councilTags(c)
		switch {
		case c.Status == queue.StatusOrphaned:
			tags += " [orphaned]"
		case c.Status == queue.StatusAuto:
			tags += " [auto]"
		case c.Mode == string(dots.ModeSuggest):
			tags += " [suggestion]"
		}
		b.WriteString(fmt.Sprintf("%s%s %s  %s · turn %d%s\n", marker, c.Glyph, style.Severity(c.Severity).Render(strings.ToUpper(c.Severity)), c.DotName, c.Turn, tags))
		text := c.Body
		if !sel {
			text = trunc(strings.ReplaceAll(text, "\n", " "), max(20, w*2))
		}
		b.WriteString(body.Render("  "+text) + "\n")
		if c.Error != "" {
			b.WriteString(style.Err.Render("  delivery failed: "+c.Error) + "\n")
		}
		b.WriteString(councilDetail(c, m.expandID == c.ID, body))
		if sel {
			if c.Mode == string(dots.ModeSuggest) {
				b.WriteString(style.Dim.Render("  x dismiss · copy the text to use it") + "\n")
			} else {
				b.WriteString(style.Dim.Render("  ⏎ send  e edit  x dismiss"+councilHint(c)) + "\n")
			}
		}
		if i == m.card {
			selEnd = lineCount(b.String()) - 1
		}
	}
	if m.showFeed {
		b.WriteString(rule("FEED", w))
		for _, e := range p.Feed {
			line := fmt.Sprintf("%3d  %-10s %s", e.Turn, trunc(e.Dot, 10), feedOutcome(e))
			if e.Detail != "" && e.Outcome == "error" {
				line += "  " + trunc(e.Detail, max(10, w-24))
			} else if c := costPart(e.Detail); c != "" && e.Outcome != state.OutcomeAsked {
				line += "  " + c
			}
			b.WriteString(trunc(line, max(10, w)) + "\n")
		}
	}
	if m.showHelp {
		b.WriteString(rule("HELP", w) + helpText + "\n" + councilHelp + "\n")
	}
	bodyText := b.String()

	// Footer: open editors and the status line, always on screen.
	b.Reset()
	if m.editing {
		b.WriteString(rule("EDIT", w) + m.editor.View() + "\n" + style.Dim.Render("ctrl+s save · esc cancel") + "\n")
	}
	if m.asking {
		b.WriteString(rule("SUMMON "+m.askSlug, w) + m.question.View() + "\n")
	}
	if m.remembering {
		switch {
		case m.reasoning:
			b.WriteString(rule("REMEMBER AS A RULE", w) + m.reason.View() + "\n")
		case m.rememberCouncil:
			b.WriteString(rule("REMEMBER", w) + "c project convention · esc cancel\n")
		default:
			b.WriteString(rule("REMEMBER", w) + "f accepted finding · r rule: don't raise this again\nc project convention · g this dot everywhere · esc cancel\n")
		}
	}
	footer := "j/k move · ⏎ send · a all · s summon · ? help"
	if m.status != "" {
		footer = m.status
	}
	b.WriteString(style.Dim.Render(footer))
	foot := b.String()

	if m.height > 0 {
		// Cut the roster first so the title and the footer stay on screen,
		// keeping one line for the body.
		sel := -1
		if m.area == areaRoster {
			sel = m.roster
		}
		roster = fitRoster(roster, m.height-lineCount(top)-lineCount(queueRule)-lineCount(foot)-1, sel, w)
	}
	head := top + strings.Join(roster, "") + queueRule
	if m.height > 0 {
		avail := max(1, m.height-lineCount(head)-lineCount(foot))
		bodyText = window(bodyText, avail, selStart, selEnd)
	}
	return head + bodyText + foot
}

// fitRoster returns at most n roster lines. When some are cut, the last line
// says how many ("+N more", cut to the width w) and the selected dot (sel, -1
// for none) stays visible; with room for one line only, the selected dot's
// line wins.
func fitRoster(lines []string, n, sel, w int) []string {
	if len(lines) <= n {
		return lines
	}
	if n <= 0 {
		return nil
	}
	if n == 1 && sel >= 0 && sel < len(lines) {
		return []string{lines[sel]}
	}
	keep := n - 1
	off := 0
	if sel >= keep {
		off = min(sel-keep+1, len(lines)-keep)
	}
	out := append([]string(nil), lines[off:off+keep]...)
	more := []rune(fmt.Sprintf("  +%d more", len(lines)-keep))
	if w > 0 && len(more) > w {
		more = more[:w]
	}
	return append(out, style.Dim.Render(string(more))+"\n")
}

// lineCount counts lines in s; a trailing line without "\n" counts too.
func lineCount(s string) int {
	n := strings.Count(s, "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// window returns at most n lines of the newline-terminated text, scrolled so
// lines [selStart, selEnd] are visible; if they don't fit, their first line
// (the card header) wins.
func window(text string, n, selStart, selEnd int) string {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= n {
		return text
	}
	off := 0
	if selStart >= 0 {
		off = max(0, min(selEnd-n+1, selStart))
	}
	off = min(off, len(lines)-n)
	return strings.Join(lines[off:off+n], "")
}

// paneCost is the header dollar part: "  $0.42/2.00" with a budget, "  $0.42"
// without one, "" before any priced run. A "?" right after the total means
// some run had no price, so the total is a lower bound.
func paneCost(cost, budget float64, unknown bool) string {
	if budget <= 0 && cost <= 0 && !unknown {
		return ""
	}
	s := "  " + usd(cost)
	if unknown {
		s += "?"
	}
	if budget > 0 {
		s += fmt.Sprintf("/%.2f", budget)
	}
	return s
}

// usd formats a dollar amount; a spend under half a cent shows as "<$0.01"
// so it doesn't look like nothing.
func usd(v float64) string {
	if v > 0 && v < 0.005 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", v)
}

// dotCost is a roster line session cost: " $0.12", " $0.12?" when some runs
// had no price, "" when nothing was spent or priced.
func dotCost(d view.DotState) string {
	if d.CostUSD == 0 && !d.CostUnknown {
		return ""
	}
	s := " " + usd(d.CostUSD)
	if d.CostUnknown {
		s += "?"
	}
	return s
}

// costPart is the "cost=..." tail of a pass/flagged feed detail.
func costPart(detail string) string {
	if i := strings.LastIndex(detail, "cost="); i >= 0 {
		return detail[i:]
	}
	return ""
}
