package summon

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/view"
)

func askingModel(fc *fakeCaller) tea.Model {
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "security", Name: "Security", Glyph: "◆"}, {Slug: "copy", Name: "Copy", Glyph: "✎"},
	}}}}))
	m, _ = m.Update(key("enter"))
	return m
}

// U8 (post-rc hardening): esc out of the question clears the typed text and a
// stale error, so the next dot doesn't inherit them.
func TestEscClearsTheQuestionAndAStaleStatus(t *testing.T) {
	m := askingModel(&fakeCaller{})
	for _, r := range "stale" {
		m, _ = m.Update(key(string(r)))
	}
	m, _ = m.Update(errMsg{errors.New("connection failed")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if v := m.View(); strings.Contains(v, "connection failed") {
		t.Fatalf("stale status after esc:\n%s", v)
	}
	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("enter"))
	if v := m.View(); strings.Contains(v, "stale") {
		t.Fatalf("the next question inherits the old text:\n%s", v)
	}
}

// U8 (post-rc hardening): ctrl+c while typing the question closes the popup.
func TestCtrlCWhileAskingQuits(t *testing.T) {
	fc := &fakeCaller{}
	m := askingModel(fc)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c must quit")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit || len(fc.calls) != 0 {
		t.Fatalf("ctrl+c must quit without summoning: calls %v", fc.calls)
	}
}
