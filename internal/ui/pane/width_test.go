package pane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// U5 (post-rc hardening): at a narrow width the card header lines and the
// footer are cut to the pane, so nothing wraps and pushes the header off.
func TestNarrowPaneCutsCardHeadersAndFooter(t *testing.T) {
	s := snap()
	s.Panes[0].Cards[0].DotName = "A Very Long Security Reviewer Name"
	s.Panes[0].Cards[0].Severity = "critical"
	for _, w := range []int{12, 16, 20, 24, 30, 40} {
		m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
		m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
		for _, l := range strings.Split(m.View(), "\n") {
			if cw := ansi.StringWidth(l); cw > w {
				t.Errorf("width %d: line %q is %d cells", w, ansi.Strip(l), cw)
			}
		}
	}
}
