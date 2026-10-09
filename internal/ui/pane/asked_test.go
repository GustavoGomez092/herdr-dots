package pane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

func askedSnap() view.Snapshot {
	s := snap()
	p := &s.Panes[0]
	p.Dots[0].Status = view.StatusConsulted
	p.Feed = []state.FeedEntry{
		{Turn: 9, Dot: "security", Outcome: state.OutcomeAsked, Detail: "warn · cost=$0.0100 tokens=900/80"},
		{Turn: 9, Dot: "frontend", Outcome: state.OutcomeAsked, Detail: "pass"},
	}
	return s
}

func TestFeedShowsAgentAsks(t *testing.T) {
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(askedSnap()))
	v := press(m, "f").View()
	if !strings.Contains(v, "◆ asked by agent · warn") || !strings.Contains(v, "◆ asked by agent · pass") {
		t.Fatalf("feed:\n%s", v)
	}
	if strings.Contains(v, "cost=$0.0100 tokens") {
		t.Fatalf("the feed line shows the severity, not the whole detail:\n%s", v)
	}
}

func TestRosterShowsConsulted(t *testing.T) {
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(askedSnap()))
	if v := m.View(); !strings.Contains(v, "consulted") {
		t.Fatalf("roster:\n%s", v)
	}
}

func TestAskedLinesNeverShowTheCostTailWhenWide(t *testing.T) {
	s := askedSnap()
	s.Panes[0].Feed = append(s.Panes[0].Feed, state.FeedEntry{Turn: 9, Dot: "council", Outcome: state.OutcomeAsked, Detail: "warn · 3 of 4 members answered · cost=$0.0200 tokens=1/2"})
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	v := press(m, "f").View()
	n := 0
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "asked by agent") {
			n++
			if strings.Contains(l, "cost=") || strings.Contains(l, "tokens") {
				t.Fatalf("cost tail on an asked line: %q", l)
			}
		}
	}
	if n != 3 {
		t.Fatalf("want 3 asked lines, got %d:\n%s", n, v)
	}
}
