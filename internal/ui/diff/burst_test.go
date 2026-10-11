package diff

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// burst is several printable keys arriving in one read: Bubble Tea merges
// them into one KeyMsg (B1).
func burst(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestABurstOfNMovesOneHunkPerRune(t *testing.T) {
	m, _ := viewer(&recCaller{}).Update(burst("nn"))
	if got := m.(Model).top; got != 8 {
		t.Fatalf("top %d after \"nn\", want the second hunk (8)", got)
	}
}

// "Ay" in one read opens the apply confirm; y must come as its own key.
func TestABurstNeverConfirmsAnApply(t *testing.T) {
	rc := &recCaller{}
	m, cmd := viewer(rc).Update(burst("Ay"))
	if !m.(Model).confirm {
		t.Fatal("A did not open the apply confirm")
	}
	if cmd != nil || m.(Model).busy {
		t.Fatal("the burst applied the patch")
	}
	if _, cmd = m.Update(keyOf("y")); cmd == nil {
		t.Fatal("a separate y must apply")
	}
}
