package manage

import (
	"context"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/view"
)

// burst is several printable keys arriving in one read: Bubble Tea merges
// them into one KeyMsg (B1).
func burst(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func fiveDots(t *testing.T) tea.Model {
	s := lib()
	s.Library = nil
	for i := 1; i <= 5; i++ {
		s.Library = append(s.Library, view.LibraryDot{Slug: fmt.Sprintf("dot%d", i), Name: fmt.Sprintf("Dot%d", i), Glyph: "◆", Backend: "claude-cli", Model: "sonnet", Mode: "queue"})
	}
	m, _ := New("w1:p1", Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil }}).Update(SnapshotMsg(s))
	return m
}

func TestABurstOfJMovesOneLibraryRowPerRune(t *testing.T) {
	m, _ := fiveDots(t).Update(burst("jjj"))
	if got := m.(Model).cursor; got != 3 {
		t.Fatalf("library cursor %d after \"jjj\", want 3", got)
	}
}

// "Dy" in one read opens the delete confirm; y must come as its own key.
func TestABurstNeverConfirmsADelete(t *testing.T) {
	m, cmd := fiveDots(t).Update(burst("Dy"))
	if got := m.(Model).scr; got != scrConfirm {
		t.Fatalf("screen %v after \"Dy\", want the delete confirm still open", got)
	}
	if cmd != nil {
		t.Fatal("the burst returned a command: it confirmed the delete")
	}
}

func TestABurstIsTypedIntoTheImportPrompt(t *testing.T) {
	m := press(fiveDots(t), "I")
	m, _ = m.Update(burst("hello"))
	if got := m.(Model).input.Value(); got != "hello" {
		t.Fatalf("import prompt %q, want hello", got)
	}
}

// The Setup pager scrolls on a burst, but y (install) needs its own key.
func TestABurstNeverInstallsFromTheSetupScreen(t *testing.T) {
	f := &setupFakes{}
	m := boot(t, f, 200, 200) // everything fits: the end is seen at once
	if m.(Model).scr != scrSetup {
		t.Fatalf("setup screen not open:\n%s", screenText(m))
	}
	m, cmd := m.Update(burst("jy"))
	if cmd != nil || m.(Model).setup.applying {
		t.Fatal("the burst installed")
	}
	if _, cmd = m.Update(key("y")); cmd == nil {
		t.Fatal("a separate y must install")
	}
}

func TestABurstScrollsTheSetupScreen(t *testing.T) {
	f := &setupFakes{}
	m := boot(t, f, 96, 12)
	m, _ = m.Update(burst("jjj"))
	if got := m.(Model).setup.pg.offset; got != 3 {
		t.Fatalf("setup offset %d after \"jjj\", want 3", got)
	}
}
