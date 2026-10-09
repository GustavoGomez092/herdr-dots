package pane

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"herdr-dots/internal/queue"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// councilHelp is appended to the help text.
const councilHelp = `C council (ask every dot)   space expand council card`

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
		if !ok || c.Kind != queue.KindCouncil || m.area != areaQueue {
			return m, nil, false
		}
		if m.expandID == c.ID {
			m.expandID = ""
		} else {
			m.expandID = c.ID
		}
		return m, nil, true
	}
	return m, nil, false
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
	if c.Kind != queue.KindCouncil {
		return ""
	}
	return fmt.Sprintf(" · %d dots", len(c.Parts))
}

// councilHint extends the selected card's key hint.
func councilHint(c queue.Card) string {
	if c.Kind != queue.KindCouncil {
		return ""
	}
	return "  space expand"
}

// councilDetail renders a council card's footer note and, when expanded, the
// question and every member's own answer. Every line is wrapped to the body
// width so the viewport's line count matches what the terminal shows.
func councilDetail(c queue.Card, expanded bool, body lipgloss.Style) string {
	if c.Kind != queue.KindCouncil {
		return ""
	}
	var b strings.Builder
	if c.Note != "" {
		b.WriteString(style.Dim.Render("  "+c.Note) + "\n")
	}
	if !expanded {
		return b.String()
	}
	if c.Question != "" {
		b.WriteString(body.Render(style.Dim.Render("  asked: "+c.Question)) + "\n")
	}
	for _, p := range c.Parts {
		label := "nothing to add"
		switch {
		case p.Failed:
			label = style.Err.Render("✗ " + p.Body)
		case p.Severity != "":
			label = style.Severity(p.Severity).Render(strings.ToUpper(p.Severity))
		}
		if p.Stance != "" {
			label += " · " + p.Stance
		}
		b.WriteString(body.Render(fmt.Sprintf("  %s %s  %s", p.Glyph, p.DotName, label)) + "\n")
		if !p.Failed && p.Body != "" {
			b.WriteString(body.Render("    "+p.Body) + "\n")
		}
	}
	return b.String()
}
