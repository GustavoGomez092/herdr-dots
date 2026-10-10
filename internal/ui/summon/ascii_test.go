package summon

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// U6 (post-rc hardening): with HERDR_DOTS_ASCII=1 the summon popup draws its
// selection marker and the council entry in ASCII too.
func TestASCIIModeDrawsMarkerAndCouncilInASCII(t *testing.T) {
	old := style.SetASCII(true)
	defer style.SetASCII(old)
	var m tea.Model = New("w1:p1", &fakeCaller{})
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "security", Name: "Security", Glyph: "◆"},
	}}}}))
	v := m.View()
	for _, glyph := range []string{"▸", "⊕", "◆"} {
		if strings.Contains(v, glyph) {
			t.Errorf("ASCII mode still draws %q:\n%s", glyph, v)
		}
	}
	if !strings.Contains(v, "> ") {
		t.Fatalf("ASCII marker missing:\n%s", v)
	}
}
