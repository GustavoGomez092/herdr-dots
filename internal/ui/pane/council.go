package pane

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// councilHelp is appended to the help text.
const councilHelp = `C council (ask every dot)   space expand council/away card`

// WithOpener sets how the pane opens the council popup (key C). The command
// wiring passes a function that opens the plugin's "council" popup for the
// target pane; without one, C only explains where to find the action.
func (m Model) WithOpener(open func(target string) error) Model {
	m.opener = open
	return m
}

// councilKeys handles the council keys. ok=false means "not a council key".
func (m Model) councilKeys(k tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch k.String() {
	case "C":
		if !m.connected {
			m.status = offlineHint
			return m, nil, true
		}
		if m.opener == nil {
			m.status = "council: run the herdr-dots.council action"
			return m, nil, true
		}
		open, target := m.opener, m.target
		m.status = "opening council…"
		return m, func() tea.Msg {
			if err := open(target); err != nil {
				return errMsg{err}
			}
			return nil
		}, true
	case " ":
		c, ok := m.selectedCard()
		if !ok || !hasParts(c) || m.area != areaQueue {
			return m, nil, false
		}
		if m.expandID == c.ID {
			m.expandID = ""
		} else {
			m.expand(c)
		}
		return m, nil, true
	}
	return m, nil, false
}

// expand shows a card's parts and remembers how many were shown.
func (m *Model) expand(c queue.Card) {
	if m.opened == nil {
		m.opened = map[string]int{}
	}
	m.opened[c.ID] = len(c.Parts)
	m.expandID = c.ID
}

// unseen reports a digest with parts the developer has not been shown: never
// expanded, or grown since (queue.AppendPart).
func (m *Model) unseen(c queue.Card) bool {
	return c.Kind == queue.KindDigest && m.opened[c.ID] < len(c.Parts)
}

// sendArgs names the cards to send and, for each digest, the part count shown
// (the engine refuses a digest whose count changed).
func sendArgs(cards []queue.Card) ipc.SendArgs {
	a := ipc.SendArgs{}
	for _, c := range cards {
		a.IDs = append(a.IDs, c.ID)
		if c.Kind == queue.KindDigest {
			if a.Parts == nil {
				a.Parts = map[string]int{}
			}
			a.Parts[c.ID] = len(c.Parts)
		}
	}
	return a
}

// councilProgress is the roster line while a council runs.
func councilProgress(c view.CouncilState) string {
	switch c.Phase {
	case view.CouncilMerging:
		return "⊕ council: merging…"
	case view.CouncilRound2:
		return fmt.Sprintf("⊕ council: round 2 %d/%d members…", c.Done, c.Total)
	}
	return fmt.Sprintf("⊕ council: %d/%d members…", c.Done, c.Total)
}

// councilTags is the card header suffix for a council card.
func councilTags(c queue.Card) string {
	if !hasParts(c) {
		return ""
	}
	if len(c.Parts) == 1 {
		return " · 1 dot"
	}
	return fmt.Sprintf(" · %d dots", len(c.Parts))
}

// councilText shows council text with its Markdown headings as plain labels:
// "## Must fix" reads "Must fix:". Display only: the agent gets the card as
// the chair wrote it.
func councilText(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		h := strings.TrimSpace(strings.TrimLeft(t, "#"))
		if h != "" && !strings.HasSuffix(h, ":") {
			h += ":"
		}
		lines[i] = h
	}
	return strings.Join(lines, "\n")
}

// councilHint extends the selected card's key hint.
func councilHint(c queue.Card) string {
	if !hasParts(c) {
		return ""
	}
	return "  space expand"
}

// councilDetail renders a council card's footer note and, when expanded, the
// question and every member's own answer. Every line is wrapped to the body
// width of a w-wide pane and indented line by line (B3), so the viewport's
// line count matches what the terminal shows.
func councilDetail(c queue.Card, expanded bool, w int) string {
	if !hasParts(c) {
		return ""
	}
	var b strings.Builder
	if c.Note != "" {
		b.WriteString(indented(style.Dim, oneLine(c.Note), w, 2) + "\n")
	}
	if !expanded {
		return b.String()
	}
	if c.Question != "" {
		b.WriteString(indented(style.Dim, "asked: "+oneLine(c.Question), w, 2) + "\n")
	}
	plain := lipgloss.NewStyle()
	for _, p := range c.Parts {
		label := "nothing to add"
		switch {
		case p.Failed:
			label = style.Err.Render("✗ " + oneLine(p.Body))
		case p.Severity != "":
			label = style.Severity(p.Severity).Render(strings.ToUpper(p.Severity))
		}
		if p.Stance != "" {
			label += " · " + oneLine(p.Stance)
		}
		b.WriteString(indented(plain, fmt.Sprintf("%s %s  %s", oneLine(p.Glyph), oneLine(p.DotName), label), w, 2) + "\n")
		if !p.Failed && p.Body != "" {
			b.WriteString(indented(plain, councilText(textutil.SanitizeModelText(p.Body)), w, 4) + "\n")
		}
	}
	return b.String()
}
