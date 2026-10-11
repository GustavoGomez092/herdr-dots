package profile

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// burst is several printable keys arriving in one read: Bubble Tea merges
// them into one KeyMsg (B1).
func burst(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestABurstOfJMovesOneHistoryRowPerRune(t *testing.T) {
	m, _ := openProfile(t, nil).Update(burst("jj"))
	if m.cursor != 2 {
		t.Fatalf("cursor %d after \"jj\", want 2", m.cursor)
	}
}
