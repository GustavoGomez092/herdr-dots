package pane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

func memSnap() view.Snapshot {
	s := snap()
	p := &s.Panes[0]
	p.ProjectName, p.MemoryEntries, p.MemoryProposals = "herdr-dots", 42, 2
	p.Dots[1].Learning = true
	p.Feed = []state.FeedEntry{
		{Turn: 7, Dot: "security", Outcome: state.OutcomeLearned, Detail: "learned 2"},
		{Turn: 8, Dot: "security", Outcome: state.OutcomeSuppressed, Detail: "similar to d:m17"},
	}
	return s
}

func memReady(fc *fakeCaller) tea.Model {
	m, _ := New("w1:p1", fc).Update(SnapshotMsg(memSnap()))
	return m
}

func TestHeaderShowsProjectAndMemoryCounts(t *testing.T) {
	m := New("w1:p1", &fakeCaller{})
	m.width = 100 // the header is cut to the width, so give it room
	mm, _ := m.Update(SnapshotMsg(memSnap()))
	v := mm.(Model).View()
	if !strings.Contains(v, "DOTS → claude (w1:p1) · herdr-dots · mem 42 (2 proposals)") || !strings.Contains(v, "budget 3/120") {
		t.Fatalf("header:\n%s", v)
	}
	if strings.Contains(ready(&fakeCaller{}).View(), "mem ") {
		t.Fatal("no memory part without a project")
	}
}

func TestLearningDotShowsLearning(t *testing.T) {
	if v := memReady(&fakeCaller{}).View(); !strings.Contains(v, "◌ learning…") {
		t.Fatalf("roster:\n%s", v)
	}
}

func TestRememberRuleWithReason(t *testing.T) {
	fc := &fakeCaller{}
	m := press(memReady(fc), "M")
	if !strings.Contains(m.View(), "r rule: don't raise this again") {
		t.Fatalf("chooser:\n%s", m.View())
	}
	press(m, "r", "internal tool", "enter")
	if len(fc.calls) != 1 || fc.calls[0].cmd != ipc.CmdRemember || fc.calls[0].args != `{"card_id":"1","as":"rule","reason":"internal tool"}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestRememberOtherKinds(t *testing.T) {
	for k, as := range map[string]string{"f": "finding", "c": "convention", "g": "global"} {
		fc := &fakeCaller{}
		press(memReady(fc), "M", k)
		if len(fc.calls) != 1 || fc.calls[0].args != `{"card_id":"1","as":"`+as+`"}` {
			t.Errorf("%s: calls %+v", k, fc.calls)
		}
	}
}

func TestRememberEscSendsNothing(t *testing.T) {
	fc := &fakeCaller{}
	m := press(memReady(fc), "M", "esc")
	press(m, "M", "r", "esc")
	if len(fc.calls) != 0 {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestRememberNeedsQueueFocusAndEngine(t *testing.T) {
	fc := &fakeCaller{}
	if v := press(memReady(fc), "tab", "M").View(); !strings.Contains(v, "tab to the queue") || len(fc.calls) != 0 {
		t.Fatalf("roster focus:\n%s", v)
	}
	m, _ := memReady(fc).Update(DisconnectedMsg{})
	if v := press(m, "M").View(); !strings.Contains(v, "offline") || len(fc.calls) != 0 {
		t.Fatalf("offline:\n%s", v)
	}
}

func TestFeedShowsLearnedAndSuppressed(t *testing.T) {
	v := press(memReady(&fakeCaller{}), "f").View()
	if !strings.Contains(v, "learned 2") || !strings.Contains(v, "suppressed (d:m17)") {
		t.Fatalf("feed:\n%s", v)
	}
}

// The chooser sits in the footer with the editors, so a short pane never
// scrolls it off.
func TestRememberChooserStaysOnScreenInShortPane(t *testing.T) {
	var m tea.Model = New("w1:p1", &fakeCaller{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 10})
	m, _ = m.Update(SnapshotMsg(memSnap()))
	v := press(m, "M").View()
	if !strings.Contains(v, "f accepted finding") || !strings.Contains(v, "DOTS →") {
		t.Fatalf("chooser and header must both show:\n%s", v)
	}
	if n := strings.Count(v, "\n") + 1; n > 10 {
		t.Fatalf("view is %d lines, want <= 10:\n%s", n, v)
	}
}
