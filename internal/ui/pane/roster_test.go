package pane

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/view"
)

// sixDots is snap() with six dots on the pane and no cards.
func sixDots() view.Snapshot {
	s := snap()
	s.Panes[0].Cards = nil
	s.Panes[0].Dots = nil
	for i := 1; i <= 6; i++ {
		s.Panes[0].Dots = append(s.Panes[0].Dots, view.DotState{
			Slug: fmt.Sprintf("dot%d", i), Name: fmt.Sprintf("Dot%d", i), Glyph: "◆", Color: "red", Mode: "queue", Status: "passed",
		})
	}
	return s
}

// When the roster doesn't fit, it is cut first ("+N more") so the header and
// the footer stay on screen.
func TestShortPaneTruncatesRosterAndKeepsHeader(t *testing.T) {
	var m tea.Model = New("w1:p1", &fakeCaller{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 8})
	m, _ = m.Update(SnapshotMsg(sixDots()))
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) > 8 {
		t.Fatalf("view is %d lines, want <= 8:\n%s", len(lines), v)
	}
	if !strings.Contains(lines[0], "DOTS →") {
		t.Fatalf("the header must be the first line:\n%s", v)
	}
	for _, want := range []string{"Dot1", "+3 more", "QUEUE (0)", "j/k move"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Dot6") {
		t.Errorf("the roster must be cut:\n%s", v)
	}
}

// The selected roster dot stays visible when the roster is cut.
func TestShortPaneKeepsSelectedRosterDotVisible(t *testing.T) {
	var m tea.Model = New("w1:p1", &fakeCaller{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 8})
	m, _ = m.Update(SnapshotMsg(sixDots()))
	m = press(m, "tab", "j", "j", "j", "j", "j") // roster, select Dot6
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) > 8 || !strings.Contains(lines[0], "DOTS →") {
		t.Fatalf("header must stay on an 8-line screen:\n%s", v)
	}
	for _, want := range []string{"Dot6", "+3 more"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}
