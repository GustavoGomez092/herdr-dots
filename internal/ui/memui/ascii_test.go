package memui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ui/style"
)

// U6 (post-rc hardening): with HERDR_DOTS_ASCII=1 the memory screen draws its
// section rules, the selection marker and cut-off rows in ASCII too.
func TestASCIIModeDrawsRulesMarkerAndCutsInASCII(t *testing.T) {
	old := style.SetASCII(true)
	defer style.SetASCII(old)
	n := 0
	m, _ := open(seed(t), "security", &n).Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	v := m.View()
	for _, glyph := range []string{"─", "▸", "…"} {
		if strings.Contains(v, glyph) {
			t.Errorf("ASCII mode still draws %q:\n%s", glyph, v)
		}
	}
	if !strings.Contains(v, "-- FINDINGS") || !strings.Contains(v, "> ") {
		t.Fatalf("ASCII rule or marker missing:\n%s", v)
	}
	v = press(m, "e").View() // the editor marks its focused field
	if strings.Contains(v, "▸") || !strings.Contains(v, "> text") {
		t.Fatalf("editor marker:\n%s", v)
	}
}
