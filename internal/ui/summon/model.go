// Package summon is the small popup that runs one attached dot right now,
// with an optional question, or convenes a council of all of them
// (council.go).
package summon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

type Caller interface {
	Call(ctx context.Context, cmd string, args any) error
}

type SnapshotMsg view.Snapshot
type errMsg struct{ err error }

type Model struct {
	target  string
	caller  Caller
	dots    []view.DotState
	cursor  int
	asking  bool
	askSlug string
	input   textinput.Model
	status  string
	width   int // terminal size from tea.WindowSizeMsg; 0 until known
	height  int

	onCouncil bool         // the selection is on the "⊕ Council" entry above the dots
	council   *councilForm // non-nil: the popup is in council mode (council.go)
}

func New(target string, c Caller) Model {
	ti := textinput.New()
	ti.Placeholder = "optional question"
	return Model{target: target, caller: c, input: ti}
}

func (m Model) Init() tea.Cmd {
	if m.council != nil {
		return m.preflight()
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SnapshotMsg:
		p, _ := view.Snapshot(msg).Pane(m.target)
		m.dots = p.Dots
		m.cursor = max(0, min(m.cursor, len(m.dots)-1))
		if len(m.dots) == 0 {
			m.onCouncil = false
		}
		if len(m.dots) == 0 && m.asking {
			m.asking = false
			m.input.Blur()
		}
		if m.council != nil {
			m = m.snapshotCouncil(p)
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.council != nil {
			m.council.question.Width = questionWidth(m.width)
		}
	case errMsg:
		m.status = msg.err.Error()
		if m.council != nil {
			m.council.sending = false
		}
	case tea.KeyMsg:
		if m.council != nil {
			return m.updateCouncil(msg)
		}
		if m.asking {
			switch msg.String() {
			case "esc":
				m.asking = false
				return m, nil
			case "enter":
				if len(m.dots) == 0 {
					return m, nil
				}
				q := strings.TrimSpace(m.input.Value())
				c, target, slug := m.caller, m.target, m.askSlug
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := c.Call(ctx, ipc.CmdSummon, ipc.SummonArgs{PaneID: target, Slug: slug, Question: q}); err != nil {
						return errMsg{err}
					}
					return tea.Quit()
				}
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.onCouncil {
				m.onCouncil = false
				return m, nil
			}
			m.cursor = min(m.cursor+1, max(0, len(m.dots)-1))
		case "k", "up":
			if m.cursor == 0 && len(m.dots) > 0 {
				m.onCouncil = true
			}
			m.cursor = max(m.cursor-1, 0)
		case "enter":
			if m.onCouncil && len(m.dots) > 0 {
				m.council = newCouncilForm(true)
				m.council.question.Width = questionWidth(m.width)
				m.status = ""
				m.council.dots = m.dots
				return m, m.preflight()
			}
			if len(m.dots) > 0 {
				m.asking = true
				m.askSlug = m.dots[m.cursor].Slug
				m.input.Focus()
				return m, textinput.Blink
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.council != nil {
		return clampWidth(m.viewCouncil(), m.width)
	}
	return clampWidth(m.viewList(), m.width)
}

func (m Model) viewList() string {
	var b strings.Builder
	b.WriteString(style.Title.Render("SUMMON A DOT → "+m.target) + "\n\n")
	if len(m.dots) == 0 {
		b.WriteString(style.Dim.Render("No dots attached to this pane. Press prefix+d to add some.") + "\n")
	}
	if len(m.dots) > 0 {
		marker := "  "
		if m.onCouncil {
			marker = style.Sym("▸") + " "
		}
		b.WriteString(marker + style.Sym("⊕") + " Council (all attached)\n")
	}
	// Fixed lines: title, blank, council entry, blank, footer, plus the optional
	// question input and status.
	fixed := 5
	if m.asking {
		fixed += 2
	}
	if m.status != "" {
		fixed += 2
	}
	from, to := rowWindow(len(m.dots), m.cursor, m.height-fixed, m.height)
	if from > 0 {
		b.WriteString(moreMarker("↑", from))
	}
	for i := from; i < to; i++ {
		d := m.dots[i]
		marker := "  "
		if i == m.cursor && !m.onCouncil {
			marker = style.Sym("▸") + " "
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", marker, style.Glyph(d.Glyph, d.Color), d.Name))
	}
	if to < len(m.dots) {
		b.WriteString(moreMarker("↓", len(m.dots)-to))
	}
	if m.asking {
		b.WriteString("\n" + m.input.View() + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + style.Err.Render(m.status) + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("enter choose · enter again to summon · esc close"))
	return b.String()
}
