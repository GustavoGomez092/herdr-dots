package pane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ui/style"
)

// U6 (post-rc hardening): with HERDR_DOTS_ASCII=1 the pane draws its rules,
// the selection marker and cut-off text in ASCII too.
func TestASCIIModeDrawsRulesAndMarkerInASCII(t *testing.T) {
	old := style.SetASCII(true)
	defer style.SetASCII(old)
	m, _ := ready(&fakeCaller{}).Update(tea.WindowSizeMsg{Width: 30, Height: 30})
	v := m.View() // the queue has focus: the selected card gets the marker
	for _, glyph := range []string{"─", "▸", "…", "◆", "▲"} {
		if strings.Contains(v, glyph) {
			t.Errorf("ASCII mode still draws %q:\n%s", glyph, v)
		}
	}
	if !strings.Contains(v, "-- QUEUE") || !strings.Contains(v, ">") {
		t.Fatalf("ASCII rule or marker missing:\n%s", v)
	}
}
