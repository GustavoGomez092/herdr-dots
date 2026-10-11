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

// I1: T reads the file off the UI goroutine, then opens the trust preview.
// The rest of the burst never reaches it, nor the library under it (n would
// open the dot editor there).
func TestABurstEndsAtTheTrustPreviewThatOpensLater(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, _, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 200}) // the whole file fits: y would trust at once
	m = press(m, "j", "j")
	fc.calls = nil
	m, cmd := m.Update(burst("Tyn"))
	if got := m.(Model).scr; got != scrLibrary {
		t.Fatalf("screen %v before the preview opens: the rest of the burst ran in the library", got)
	}
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		m, cmd = m.Update(msg)
	}
	if m.(Model).scr != scrTrust {
		t.Fatalf("the trust preview must be open:\n%s", m.View())
	}
	if len(fc.calls) != 0 {
		t.Fatalf("the burst's y trusted the dot: %v", fc.calls)
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
