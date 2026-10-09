package summon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/council"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// councilForm is the council popup: a question and a member checklist.
type councilForm struct {
	fromList  bool // opened from the summon list: esc goes back to it
	question  textinput.Model
	onList    bool // focus: member list (true) or question (false)
	cursor    int
	selected  map[string]bool
	seeded    bool // selection initialised from the first preflight
	preflight *council.Preflight
	request   string // id of this popup's preflight request (ipc.CouncilArgs.Request)
	ownSeq    uint64 // Seq of the preflight that answered it; 0 until it arrives
	dots      []view.DotState
	sending   bool // a convene call is in flight: enter does nothing until it replies
}

func newCouncilForm(fromList bool) *councilForm {
	ti := textinput.New()
	ti.Placeholder = "optional question (empty: review the recent turns)"
	ti.Focus()
	return &councilForm{fromList: fromList, question: ti, selected: map[string]bool{}}
}

// NewCouncil opens the popup straight in council mode (action "council",
// pane key C). Init asks the engine for a preflight.
func NewCouncil(target string, c Caller) Model {
	m := New(target, c)
	m.council = newCouncilForm(false)
	return m
}

// preflight asks the engine for a preflight under a fresh request id.
func (m Model) preflight() tea.Cmd {
	c, target := m.caller, m.target
	m.council.request = fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	m.council.ownSeq = 0
	request := m.council.request
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Call(ctx, ipc.CmdCouncilPreflight, ipc.CouncilArgs{PaneID: target, Request: request}); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// eligible reports whether the engine's preflight lets slug sit on the council.
func (f *councilForm) eligible(slug string) bool {
	if f.preflight == nil {
		return false
	}
	for _, s := range f.preflight.Members {
		if s == slug {
			return true
		}
	}
	return false
}

func (f *councilForm) chosen() []string {
	out := []string{}
	for _, d := range f.dots {
		if f.selected[d.Slug] && f.eligible(d.Slug) {
			out = append(out, d.Slug)
		}
	}
	return out
}

// current is the preflight narrowed to the selection (nil before the engine answers).
func (f *councilForm) current() *council.Preflight {
	if f.preflight == nil {
		return nil
	}
	p := f.preflight.Select(f.chosen())
	return &p
}

func (m Model) snapshotCouncil(p view.Pane) Model {
	f := m.council
	f.dots = p.Dots
	f.cursor = max(0, min(f.cursor, len(f.dots)-1))
	if p.CouncilPreflight != nil && f.fresh(*p.CouncilPreflight) {
		pf := *p.CouncilPreflight
		f.preflight = &pf
		if !f.seeded {
			for _, s := range pf.Members {
				f.selected[s] = true
			}
			f.seeded = true
		}
	} else {
		f.preflight = nil // never show a stale estimate; the ticks stay
	}
	return m
}

// fresh reports a preflight this popup may show: the answer to its own
// request, or one computed after it. A preflight an earlier popup left in the
// snapshot is not. An unstamped preflight (Seq 0, an engine without
// sequence numbers) is taken as is.
func (f *councilForm) fresh(pf council.Preflight) bool {
	if pf.Seq == 0 {
		return true
	}
	if f.ownSeq == 0 && f.request != "" && pf.Request == f.request {
		f.ownSeq = pf.Seq
	}
	return f.ownSeq > 0 && pf.Seq >= f.ownSeq
}

func (m Model) updateCouncil(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.council
	m.status = ""
	switch msg.String() {
	case "esc":
		if f.fromList {
			m.council = nil
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+c":
		return m, tea.Quit
	case "tab":
		f.onList = !f.onList
		if f.onList {
			f.question.Blur()
			return m, nil
		}
		f.question.Focus()
		return m, textinput.Blink
	case "enter":
		cur := f.current()
		if cur == nil || !cur.Allowed || f.sending {
			return m, nil // the view already shows "checking budgets…" or the refusal
		}
		f.sending = true
		c, target, q, slugs := m.caller, m.target, strings.TrimSpace(f.question.Value()), cur.Members
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.Call(ctx, ipc.CmdCouncil, ipc.CouncilArgs{PaneID: target, Question: q, Slugs: slugs}); err != nil {
				return errMsg{err}
			}
			return tea.Quit()
		}
	}
	if !f.onList {
		var cmd tea.Cmd
		f.question, cmd = f.question.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "j", "down":
		f.cursor = min(f.cursor+1, max(0, len(f.dots)-1))
	case "k", "up":
		f.cursor = max(f.cursor-1, 0)
	case " ":
		if f.cursor < len(f.dots) {
			if s := f.dots[f.cursor].Slug; f.eligible(s) {
				f.selected[s] = !f.selected[s]
			}
		}
	}
	return m, nil
}

func (m Model) viewCouncil() string {
	f := m.council
	var b strings.Builder
	b.WriteString(style.Title.Render("COUNCIL → "+m.target) + "\n\n")
	b.WriteString(f.question.View() + "\n\n")
	if len(f.dots) == 0 {
		b.WriteString(style.Dim.Render("No dots attached to this pane. Press prefix+d to add some.") + "\n")
	}
	// Fixed lines: title, blank, question, blank, blank, estimate, footer, plus status.
	fixed := 7
	if m.status != "" {
		fixed++
	}
	from, to := rowWindow(len(f.dots), f.cursor, m.height-fixed, m.height)
	if from > 0 {
		b.WriteString(moreMarker("↑", from))
	}
	for i := from; i < to; i++ {
		d := f.dots[i]
		marker := "  "
		if f.onList && i == f.cursor {
			marker = "▸ "
		}
		box := "[ ]"
		if f.selected[d.Slug] && f.eligible(d.Slug) {
			box = "[x]"
		}
		line := fmt.Sprintf("%s%s %s %s", marker, box, style.Glyph(d.Glyph, d.Color), d.Name)
		if f.preflight != nil && !f.eligible(d.Slug) {
			reason := f.preflight.Reasons[d.Slug]
			if reason == "" {
				reason = "not eligible"
			}
			line = style.Dim.Render(line + " (" + reason + ")")
		}
		b.WriteString(line + "\n")
	}
	if to < len(f.dots) {
		b.WriteString(moreMarker("↓", len(f.dots)-to))
	}
	b.WriteString("\n")
	switch cur := f.current(); {
	case cur == nil:
		b.WriteString(style.Dim.Render("checking budgets…") + "\n")
	case !cur.Allowed:
		b.WriteString(style.Err.Render(cur.Refusal) + "\n")
	default:
		b.WriteString(style.Dim.Render(estimateLine(*cur)) + "\n")
	}
	if m.status != "" {
		b.WriteString(style.Err.Render(m.status) + "\n")
	}
	b.WriteString(style.Dim.Render("enter convene · space toggle member · tab focus · esc cancel"))
	return b.String()
}

func estimateLine(p council.Preflight) string {
	calls := "calls"
	if p.EstimatedCalls == 1 {
		calls = "call"
	}
	s := fmt.Sprintf("estimate: %d %s (%d dots × %d round", p.EstimatedCalls, calls, len(p.Members), p.Rounds)
	if p.Rounds != 1 {
		s += "s"
	}
	if p.Chair && len(p.Members) >= 2 {
		s += " + chair"
	}
	s += fmt.Sprintf(") · %d left", p.PaneLeft)
	if p.ChairUnavailable != "" && len(p.Members) >= 2 {
		s += " · chair unavailable (" + p.ChairUnavailable + ")"
	}
	if p.PaneBudgetUSD > 0 { // Phase 4: API dots spend dollars too
		s += fmt.Sprintf(" · $%.2f of $%.2f spent", p.PaneCostUSD, p.PaneBudgetUSD)
	}
	return s
}

// questionWidth is the question input's visible width for a terminal width
// (the prompt takes two columns); 0 leaves it unbounded until a size arrives.
func questionWidth(termWidth int) int {
	if termWidth <= 0 {
		return 0
	}
	return max(10, termWidth-4)
}

// rowWindow picks the slice [from, to) of n rows to show so the cursor row is
// visible in avail lines, leaving room for the "more" markers. height 0 (size
// unknown) shows everything.
func rowWindow(n, cursor, avail, height int) (from, to int) {
	if n <= 0 || height <= 0 || n <= avail {
		return 0, n
	}
	size := max(1, avail-2) // two lines reserved for the markers
	from = max(0, min(cursor-size/2, n-size))
	return from, min(n, from+size)
}

func moreMarker(arrow string, n int) string {
	return style.Dim.Render(fmt.Sprintf("  %s %d more", arrow, n)) + "\n"
}

// clampWidth cuts every line of s to width columns (0 means unknown).
func clampWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}
