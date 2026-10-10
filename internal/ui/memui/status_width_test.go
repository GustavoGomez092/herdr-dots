package memui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// U17 (post-rc hardening): a long status line wraps inside the popup instead
// of overflowing the viewport, and every word of it stays on screen.
func TestLongStatusWrapsToTheViewport(t *testing.T) {
	n := 0
	m, _ := open(seed(t), "security", &n).Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	long := "memory write failed: open /very/long/state/dir/memory/g-0123456789abcdef/dot-security.toml: permission denied"
	m, _ = m.Update(changedMsg{err: errors.New(long)})
	v := m.View()
	lines := strings.Split(v, "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "memory write failed") {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("status missing:\n%s", v)
	}
	for _, l := range lines[start:] {
		if w := ansi.StringWidth(l); w > 40 {
			t.Errorf("status line %q is %d cells wide, popup is 40", ansi.Strip(l), w)
		}
	}
	if !strings.Contains(flat(v), "toml: permission denied") {
		t.Fatalf("the end of the status was lost:\n%s", v)
	}
}

// U17 (post-rc hardening): an editor warning doesn't outlive the editor.
func TestEscFromTheEditorClearsItsStatus(t *testing.T) {
	n := 0
	m := press(open(seed(t), "security", &n), "e", "tab", "ctrl+u", "bogus", "ctrl+s")
	if !strings.Contains(m.View(), "kind must be one of") {
		t.Fatalf("editor warning:\n%s", m.View())
	}
	if v := press(m, "esc").View(); strings.Contains(v, "kind must be one of") {
		t.Fatalf("editor warning after esc:\n%s", v)
	}
}

// flat is v without styling, with every run of white space (a wrapped line
// break included) folded to one space.
func flat(v string) string { return strings.Join(strings.Fields(ansi.Strip(v)), " ") }
