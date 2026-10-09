package pane

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/view"
)

type call struct {
	cmd  string
	args string
}

type fakeCaller struct{ calls []call }

func (f *fakeCaller) Call(_ context.Context, cmd string, args any) error {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, call{cmd, string(b)})
	return nil
}

func snap() view.Snapshot {
	return view.Snapshot{Panes: []view.Pane{{
		PaneID: "w1:p1", Agent: "claude", Calls: 3, Budget: 120,
		Dots: []view.DotState{
			{Slug: "security", Name: "Security", Glyph: "◆", Color: "red", Mode: "queue", Status: "thinking"},
			{Slug: "frontend", Name: "Frontend", Glyph: "▲", Color: "cyan", Mode: "suggest", Status: "passed"},
		},
		Cards: []queue.Card{
			{ID: "1", Dot: "security", DotName: "Security", Glyph: "◆", Severity: "critical", Body: "JWT in localStorage", Mode: "queue", Status: queue.StatusPending, Turn: 4},
			{ID: "2", Dot: "frontend", DotName: "Frontend", Glyph: "▲", Severity: "info", Body: "Add a skeleton", Mode: "suggest", Status: queue.StatusPending, Turn: 4},
		},
	}}}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press sends a key and runs any returned command, like the Bubble Tea runtime.
func press(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		if cmd != nil {
			if msg := cmd(); msg != nil {
				m, _ = m.Update(msg)
			}
		}
	}
	return m
}

func ready(fc *fakeCaller) tea.Model {
	m, _ := New("w1:p1", fc).Update(SnapshotMsg(snap()))
	return m
}

func TestViewShowsRosterAndQueue(t *testing.T) {
	v := ready(&fakeCaller{}).View()
	for _, want := range []string{"claude", "w1:p1", "Security", "Frontend", "thinking", "QUEUE (2)", "JWT in localStorage", "CRITICAL", "budget 3/120"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q\n%s", want, v)
		}
	}
}

func TestEnterSendsSelectedCard(t *testing.T) {
	fc := &fakeCaller{}
	press(ready(fc), "enter")
	if len(fc.calls) != 1 || fc.calls[0].cmd != ipc.CmdSend || fc.calls[0].args != `{"ids":["1"]}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestSuggestCardCannotBeSent(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), "j", "enter")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "copy") {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestSendAllSkipsSuggestions(t *testing.T) {
	fc := &fakeCaller{}
	press(ready(fc), "a")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"ids":["1"]}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestDismissEditSummonModePause(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), "x")
	m = press(m, "e", "!", "ctrl+s")
	m = press(m, "tab") // focus roster (Security selected)
	m = press(m, "m")
	m = press(m, "p")
	m = press(m, "s", "w", "h", "y", "enter")
	want := []string{ipc.CmdDismiss, ipc.CmdEdit, ipc.CmdSetMode, ipc.CmdPause, ipc.CmdSummon}
	if len(fc.calls) != len(want) {
		t.Fatalf("calls %+v", fc.calls)
	}
	for i, c := range want {
		if fc.calls[i].cmd != c {
			t.Fatalf("call %d = %s, want %s (%+v)", i, fc.calls[i].cmd, c, fc.calls)
		}
	}
	if fc.calls[1].args != `{"id":"1","body":"JWT in localStorage!"}` {
		t.Errorf("edit args %s", fc.calls[1].args)
	}
	if fc.calls[2].args != `{"slug":"security","mode":"inject"}` {
		t.Errorf("mode args %s", fc.calls[2].args)
	}
	if fc.calls[3].args != `{"pane_id":"w1:p1","slug":"security","paused":true}` {
		t.Errorf("pause args %s", fc.calls[3].args)
	}
	if fc.calls[4].args != `{"pane_id":"w1:p1","slug":"security","question":"why"}` {
		t.Errorf("summon args %s", fc.calls[4].args)
	}
	_ = m
}

func TestDotActionsNeedRosterFocus(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), "d")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "tab to the roster to act on a dot") {
		t.Fatalf("calls %+v\n%s", fc.calls, m.View())
	}
	press(m, "tab", "d")
	if len(fc.calls) != 1 || fc.calls[0].cmd != ipc.CmdDetach {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestCardActionsNeedQueueFocus(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), "tab", "x", "enter", "e")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "tab to the queue to act on a card") {
		t.Fatalf("calls %+v\n%s", fc.calls, m.View())
	}
}

func TestDisconnectedBannerPersistsAndBlocksActions(t *testing.T) {
	fc := &fakeCaller{}
	m := ready(fc)
	m, _ = m.Update(DisconnectedMsg{})
	m = press(m, "j", "enter", "a", "x")
	m = press(m, "tab", "d", "p")
	if !strings.Contains(m.View(), "engine down") {
		t.Fatal("banner must survive keypresses")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls while disconnected: %+v", fc.calls)
	}
	m, _ = m.Update(SnapshotMsg(snap()))
	if strings.Contains(m.View(), "engine down") {
		t.Fatal("snapshot should clear banner")
	}
}

func queueCard(id, sev string) queue.Card {
	return queue.Card{ID: id, Dot: "security", DotName: "Security", Glyph: "◆", Severity: sev, Body: "card " + id, Mode: "queue", Status: queue.StatusPending, Turn: 1}
}

// A new higher-severity card shifts indexes; ⏎ must still act on the card the
// user selected.
func TestSelectionFollowsCardIDAcrossSnapshots(t *testing.T) {
	fc := &fakeCaller{}
	s := snap()
	s.Panes[0].Cards = []queue.Card{queueCard("a", "warn"), queueCard("b", "info")}
	m, _ := New("w1:p1", fc).Update(SnapshotMsg(s))
	m = press(m, "j") // select b
	s2 := snap()
	s2.Panes[0].Cards = []queue.Card{queueCard("c", "critical"), queueCard("a", "warn"), queueCard("b", "info")}
	m, _ = m.Update(SnapshotMsg(s2))
	press(m, "enter")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"ids":["b"]}` {
		t.Fatalf("enter must send the selected card b, calls %+v", fc.calls)
	}
	fc.calls = nil
	press(m, "x")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"id":"b"}` {
		t.Fatalf("x must dismiss the selected card b, calls %+v", fc.calls)
	}
}

func TestSelectionFollowsCardWhenItDisappears(t *testing.T) {
	fc := &fakeCaller{}
	s := snap()
	s.Panes[0].Cards = []queue.Card{queueCard("a", "warn"), queueCard("b", "info")}
	m, _ := New("w1:p1", fc).Update(SnapshotMsg(s))
	m = press(m, "j") // select b
	s2 := snap()
	s2.Panes[0].Cards = []queue.Card{queueCard("a", "warn")}
	m, _ = m.Update(SnapshotMsg(s2))
	press(m, "enter")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"ids":["a"]}` {
		t.Fatalf("a vanished selection clamps to a remaining card, calls %+v", fc.calls)
	}
}

func TestRosterSelectionFollowsDotSlug(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), "tab", "j") // roster: Frontend
	s := snap()
	s.Panes[0].Dots = append([]view.DotState{{Slug: "aaa", Name: "Aaa", Glyph: "★", Mode: "queue", Status: "idle"}}, s.Panes[0].Dots...)
	m, _ = m.Update(SnapshotMsg(s))
	press(m, "d")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"pane_id":"w1:p1","slug":"frontend"}` {
		t.Fatalf("d must detach the selected dot frontend, calls %+v", fc.calls)
	}
}

// The pane must keep the header, roster and selected card on screen.
func TestViewportKeepsHeaderRosterAndSelectedCard(t *testing.T) {
	fc := &fakeCaller{}
	s := snap()
	long := strings.Repeat("a long finding that wraps across several lines ", 6)
	s.Panes[0].Cards = nil
	for _, id := range []string{"1", "2", "3", "4", "5", "6"} {
		c := queueCard(id, "warn")
		c.DotName, c.Body = "Dot"+id, long
		s.Panes[0].Cards = append(s.Panes[0].Cards, c)
	}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 15})
	m, _ = m.Update(SnapshotMsg(s))
	m = press(m, "j", "j", "j", "j") // select Dot5
	v := m.View()
	for _, want := range []string{"DOTS →", "Security", "Frontend", "▸◆", "Dot5 · turn 1"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	if n := strings.Count(v, "\n") + 1; n > 15 {
		t.Errorf("view is %d lines, want <= 15:\n%s", n, v)
	}
	if strings.Contains(v, "Dot1 · turn 1") {
		t.Errorf("cards far above the selection should scroll off:\n%s", v)
	}
}

func TestEmptyAndDisconnectedStates(t *testing.T) {
	m, _ := New("w1:p9", &fakeCaller{}).Update(SnapshotMsg(snap()))
	if !strings.Contains(m.View(), "prefix+d") {
		t.Fatal("empty pane should hint how to add dots")
	}
	m, _ = m.Update(DisconnectedMsg{})
	if !strings.Contains(m.View(), "engine down") {
		t.Fatal("disconnected banner")
	}
}
