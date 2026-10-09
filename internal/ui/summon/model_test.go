package summon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

type fakeCaller struct {
	calls []string
	err   error
}

func (f *fakeCaller) Call(_ context.Context, cmd string, args any) error {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, cmd+" "+string(b))
	return f.err
}

func key(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPickDotAskAndSummon(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "security", Name: "Security", Glyph: "◆"}, {Slug: "copy", Name: "Copy", Glyph: "✎"},
	}}}}))
	if !strings.Contains(m.View(), "Copy") {
		t.Fatal(m.View())
	}
	var finalMsg tea.Msg
	for _, k := range []string{"j", "enter", "o", "k", "?", "enter"} {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		if cmd != nil {
			if msg := cmd(); msg != nil {
				finalMsg = msg
				if _, quit := msg.(tea.QuitMsg); !quit {
					m, _ = m.Update(msg)
				}
			}
		}
	}
	if len(fc.calls) != 1 || fc.calls[0] != ipc.CmdSummon+` {"pane_id":"w1:p1","slug":"copy","question":"ok?"}` {
		t.Fatalf("calls %v", fc.calls)
	}
	if _, ok := finalMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", finalMsg)
	}
}

func TestNoDotsAttached(t *testing.T) {
	var m tea.Model = New("w1:p1", &fakeCaller{})
	m, _ = m.Update(SnapshotMsg(view.Snapshot{}))
	if !strings.Contains(m.View(), "prefix+d") {
		t.Fatal(m.View())
	}
}

func TestCallerError(t *testing.T) {
	fc := &fakeCaller{err: errors.New("connection failed")}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "test", Name: "Test", Glyph: "◆"},
	}}}}))
	// Start asking
	m, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("expected command to start asking")
	}
	// Type a question
	m, _ = m.Update(key("?"))
	// Press enter to summon
	m, cmd = m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("expected command for summon")
	}
	msg := cmd()
	if _, isQuit := msg.(tea.QuitMsg); isQuit {
		t.Fatal("should not quit on error")
	}
	// Update with error message
	m, _ = m.Update(msg)
	view := m.View()
	if !strings.Contains(view, "connection failed") {
		t.Fatalf("error should be shown in view: %s", view)
	}
}

func TestEscWhileAsking(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "test", Name: "Test", Glyph: "◆"},
	}}}}))
	// Start asking
	m, _ = m.Update(key("enter"))
	// Type a question
	m, _ = m.Update(key("?"))
	// Press esc to return to list
	m, cmd := m.Update(key("esc"))
	if cmd != nil {
		t.Fatal("esc should not produce a command")
	}
	// Verify we're back to the list (not asking)
	view := m.View()
	if !strings.Contains(view, "Test") {
		t.Fatalf("should show dots list: %s", view)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("should not have called anything: %v", fc.calls)
	}
}

func TestSnapshotWithNoDotsWhileAsking(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "test", Name: "Test", Glyph: "◆"},
	}}}}))
	// Start asking
	m, _ = m.Update(key("enter"))
	// Type a question
	m, _ = m.Update(key("?"))
	// Deliver snapshot with no dots
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1"}}}))
	// Press enter - should not panic, should not call
	m, cmd := m.Update(key("enter"))
	if cmd != nil {
		t.Fatal("should not produce a command when no dots")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("should not have called anything: %v", fc.calls)
	}
}
