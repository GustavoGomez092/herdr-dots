package pane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const longBody = "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo sierra tango"

// linesWith returns the screen lines (ANSI stripped) holding any of words.
func linesWith(v string, words ...string) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(v), "\n") {
		for _, w := range words {
			if strings.Contains(l, w) {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

func assertIndented(t *testing.T, what string, lines []string, indent string, min int) {
	t.Helper()
	if len(lines) < min {
		t.Fatalf("%s: %d lines, want at least %d wrapped lines: %q", what, len(lines), min, lines)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, indent) || strings.HasPrefix(l, indent+" ") {
			t.Errorf("%s: line %q does not start with exactly %d spaces", what, l, len(indent))
		}
	}
}

// B3: every wrapped line of the selected card's text keeps the card indent.
func TestWrappedCardTextKeepsItsIndent(t *testing.T) {
	s := snap()
	s.Panes[0].Cards[0].Body = longBody
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 60})
	assertIndented(t, "card text", linesWith(m.View(), "alpha", "delta", "india", "oscar", "tango"), "  ", 4)
}

// B3: and a delivery error under a card.
func TestWrappedDeliveryErrorKeepsItsIndent(t *testing.T) {
	s := snap()
	s.Panes[0].Cards[0].Error = strings.ToUpper(longBody)
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 60})
	assertIndented(t, "delivery error", linesWith(m.View(), "delivery", "ALPHA", "DELTA", "INDIA", "OSCAR", "TANGO"), "  ", 4)
}

// B3: so does the draft confirm's instruction.
func TestWrappedDraftInstructionKeepsItsIndent(t *testing.T) {
	m := press(patchReady(&fakeCaller{}, patchSnap(), nil), "w", "e", longBody, "ctrl+s")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 60})
	assertIndented(t, "instruction", linesWith(m.View(), "alpha", "delta", "india", "oscar", "tango"), "  ", 4)
}

// B3: the council note and an expanded member's answer keep theirs too.
func TestWrappedCouncilDetailKeepsItsIndent(t *testing.T) {
	s := councilSnapshot(nil)
	s.Panes[0].Cards[0].Note = "merged without chair: " + longBody
	s.Panes[0].Cards[0].Parts[0].Body = "ZULU " + strings.ToUpper(longBody)
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 200})
	assertIndented(t, "council note", linesWith(m.View(), "merged", "alpha", "delta", "india", "oscar", "tango"), "  ", 4)
	m = press(m, " ")
	assertIndented(t, "member answer", linesWith(m.View(), "ZULU", "ALPHA", "DELTA", "INDIA", "OSCAR", "TANGO"), "    ", 4)
}
