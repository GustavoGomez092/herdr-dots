package summon

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/view"
)

// U7 (post-rc hardening): a second enter while the first summon is still in
// flight must not summon again (one extra budgeted run). After a failed call
// enter works again.
func TestDoubleEnterSummonsOnce(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "security", Name: "Security", Glyph: "◆"},
	}}}}))
	m, _ = m.Update(key("enter")) // pick the dot: the question input opens
	m, first := m.Update(key("enter"))
	m, second := m.Update(key("enter")) // before the first call returned
	if first == nil {
		t.Fatal("the first enter must summon")
	}
	if second != nil {
		t.Fatal("a second enter while the summon is in flight must do nothing")
	}
	first()
	if len(fc.calls) != 1 {
		t.Fatalf("calls %v", fc.calls)
	}
	m, _ = m.Update(errMsg{errors.New("connection failed")})
	if _, retry := m.Update(key("enter")); retry == nil {
		t.Fatal("enter must work again after a failed summon")
	}
}
