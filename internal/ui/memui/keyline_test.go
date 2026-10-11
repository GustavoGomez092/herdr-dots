package memui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// B6 (the same bug on the memory screen): a "Too small" export screen fits
// the popup's height, so the terminal never drops its title.
func TestATooSmallExportScreenKeepsItsTop(t *testing.T) {
	for _, sz := range [][2]int{{20, 8}, {20, 6}, {60, 5}} {
		w, h := sz[0], sz[1]
		m := openAt(seed(t), herdrDots, "security", t.TempDir())
		m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		v := ansi.Strip(press(m, "E").View())
		lines := strings.Split(v, "\n")
		if len(lines) > h {
			t.Errorf("%dx%d: %d lines, the terminal drops the top:\n%s", w, h, len(lines), v)
		}
		if first := strings.TrimSpace(lines[0]); !strings.HasPrefix(first, "EXPORT") && !strings.HasPrefix(first, "Too small") {
			t.Errorf("%dx%d: first line %q:\n%s", w, h, first, v)
		}
	}
}

// The browse key line ("x forget  X forget all ...") is hidden while a
// prompt is open: its keys don't apply there.
func TestTheBrowseKeyLineHidesUnderPrompts(t *testing.T) {
	const keyLine = "X forget all"
	flat := func(m tea.Model) string { return strings.Join(strings.Fields(ansi.Strip(m.View())), " ") }
	m := openAt(seed(t), herdrDots, "security", t.TempDir())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if !strings.Contains(flat(m), keyLine) {
		t.Fatalf("browse shows the key line:\n%s", m.View())
	}
	for _, open := range []string{"E", "I", "x", "/"} {
		p := press(m, open)
		if v := flat(p); strings.Contains(v, keyLine) {
			t.Errorf("%s: the browse key line is still shown:\n%s", open, p.View())
		}
		if v := flat(press(p, "esc", "j")); !strings.Contains(v, keyLine) { // j clears esc's status line
			t.Errorf("%s then esc: the key line is back in browse:\n%s", open, v)
		}
	}
}
