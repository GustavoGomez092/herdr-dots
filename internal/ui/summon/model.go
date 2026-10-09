// Package summon is the small popup that runs one attached dot right now,
// with an optional question.
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
	target string
	caller Caller
	dots   []view.DotState
	cursor int
	asking bool
	input  textinput.Model
	status string
}

func New(target string, c Caller) Model {
	ti := textinput.New()
	ti.Placeholder = "optional question"
	return Model{target: target, caller: c, input: ti}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SnapshotMsg:
		p, _ := view.Snapshot(msg).Pane(m.target)
		m.dots = p.Dots
		m.cursor = max(0, min(m.cursor, len(m.dots)-1))
	case errMsg:
		m.status = msg.err.Error()
	case tea.KeyMsg:
		if m.asking {
			switch msg.String() {
			case "esc":
				m.asking = false
				return m, nil
			case "enter":
				d := m.dots[m.cursor]
				q := strings.TrimSpace(m.input.Value())
				c, target := m.caller, m.target
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := c.Call(ctx, ipc.CmdSummon, ipc.SummonArgs{PaneID: target, Slug: d.Slug, Question: q}); err != nil {
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
			m.cursor = min(m.cursor+1, max(0, len(m.dots)-1))
		case "k", "up":
			m.cursor = max(m.cursor-1, 0)
		case "enter":
			if len(m.dots) > 0 {
				m.asking = true
				m.input.Focus()
				return m, textinput.Blink
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(style.Title.Render("SUMMON A DOT → "+m.target) + "\n\n")
	if len(m.dots) == 0 {
		b.WriteString(style.Dim.Render("No dots attached to this pane. Press prefix+d to add some.") + "\n")
	}
	for i, d := range m.dots {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", marker, style.Glyph(d.Glyph, d.Color), d.Name))
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
