package pane

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
)

// sendKeys sends real key messages (a space is its own KeySpace message, so
// "w", space, "y" typed fast arrives as three messages, not one burst).
func sendKeys(m tea.Model, ks ...tea.KeyMsg) tea.Model {
	for _, k := range ks {
		m = update(m, k)
	}
	return m
}

var space = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}

// I2: any key but the draft confirm's own keys cancels it, so "w", space,
// "y" never starts a draft.
func TestTheDraftConfirmCancelsOnAnyOtherKey(t *testing.T) {
	fc := &fakeCaller{}
	m := sendKeys(patchReady(fc, patchSnap(), nil), key("w"), space, key("y"))
	if m.(Model).draft != nil {
		t.Fatal("space must cancel the draft confirm")
	}
	for _, c := range fc.calls {
		if c.cmd == ipc.CmdDraft {
			t.Fatalf("a draft started: %+v", fc.calls)
		}
	}
}

// I2: any key but a choice cancels the remember chooser, so "M", space,
// "f" saves nothing.
func TestTheRememberChooserCancelsOnAnyOtherKey(t *testing.T) {
	fc := &fakeCaller{}
	m := sendKeys(ready(fc), key("M"), space, key("f"))
	if m.(Model).remembering {
		t.Fatal("space must cancel the chooser")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls %+v: something was remembered", fc.calls)
	}
}
