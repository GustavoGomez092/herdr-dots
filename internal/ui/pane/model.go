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
	showFeed  bool
	showHelp  bool
	editing   bool
	editID    string
	editor    textarea.Model
	asking    bool
	askSlug   string
	question  textinput.Model
	width     int
	status    string
}

func New(target string, c Caller) Model {
	ta := textarea.New()
	ta.SetHeight(6)
	ta.ShowLineNumbers = false
	ti := textinput.New()
	ti.Placeholder = "optional question, enter to summon"
	return Model{target: target, caller: c, editor: ta, question: ti}
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

func (m *Model) clamp() {
	p := m.pane()
	m.card = max(0, min(m.card, len(p.Cards)-1))
	m.roster = max(0, min(m.roster, len(p.Dots)-1))
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
		m.width = msg.Width
		m.editor.SetWidth(max(20, msg.Width-2))
		return m, nil
	case SnapshotMsg:
		m.snap, m.connected = view.Snapshot(msg), true
		if m.status == "engine down, restarting…" {
			m.status = ""
		}
		m.clamp()
		return m, nil
	case DisconnectedMsg:
		m.connected = false
		m.status = "engine down, restarting…"
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

func (m Model) updateKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
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
s summon dot   m cycle mode   p pause   d detach
r reset errored dot   f feed   ? help   q close`

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
	head := fmt.Sprintf("DOTS → %s (%s)", agent, m.target)
	if ok {
		head += fmt.Sprintf("  budget %d/%d", p.Calls, p.Budget)
	}
	if m.snap.PausedAll {
		head += "  ⏸ all paused"
	}
	b.WriteString(style.Title.Render(head) + "\n")
	if p.Source == "scrollback" {
		b.WriteString(style.Dim.Render("(reading scrollback: transcript unavailable)") + "\n")
	}
	if !m.connected && m.status != "" {
		b.WriteString(style.Err.Render(m.status) + "\n")
	}
	if len(p.Dots) == 0 {
		b.WriteString(style.Dim.Render("No dots here yet. Press prefix+d to add some.") + "\n")
	}
	for i, d := range p.Dots {
		line := fmt.Sprintf("%s %-12s %-8s %s", style.Glyph(d.Glyph, d.Color), trunc(d.Name, 12), d.Mode, d.Status)
		if m.area == areaRoster && i == m.roster {
			line = style.Selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(rule(fmt.Sprintf("QUEUE (%d)", len(p.Cards)), w))
	body := lipgloss.NewStyle().Width(max(10, w-2))
	for i, c := range p.Cards {
		sel := m.area == areaQueue && i == m.card
		marker := " "
		if sel {
			marker = "▸"
		}
		tags := ""
		switch {
		case c.Status == queue.StatusOrphaned:
			tags = " [orphaned]"
		case c.Status == queue.StatusAuto:
			tags = " [auto]"
		case c.Mode == string(dots.ModeSuggest):
			tags = " [suggestion]"
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
		if sel {
			if c.Mode == string(dots.ModeSuggest) {
				b.WriteString(style.Dim.Render("  x dismiss · copy the text to use it") + "\n")
			} else {
				b.WriteString(style.Dim.Render("  ⏎ send  e edit  x dismiss") + "\n")
			}
		}
	}
	if m.editing {
		b.WriteString(rule("EDIT", w) + m.editor.View() + "\n" + style.Dim.Render("ctrl+s save · esc cancel") + "\n")
	}
	if m.asking {
		b.WriteString(rule("SUMMON "+m.askSlug, w) + m.question.View() + "\n")
	}
	if m.showFeed {
		b.WriteString(rule("FEED", w))
		for _, e := range p.Feed {
			line := fmt.Sprintf("%3d  %-10s %s", e.Turn, trunc(e.Dot, 10), e.Outcome)
			if e.Detail != "" && e.Outcome == "error" {
				line += "  " + trunc(e.Detail, max(10, w-24))
			}
			b.WriteString(line + "\n")
		}
	}
	if m.showHelp {
		b.WriteString(rule("HELP", w) + helpText + "\n")
	}
	footer := "j/k move · ⏎ send · a all · s summon · ? help"
	if m.connected && m.status != "" {
		footer = m.status
	}
	b.WriteString(style.Dim.Render(footer))
	return b.String()
}
