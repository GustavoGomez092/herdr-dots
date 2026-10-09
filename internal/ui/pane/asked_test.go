package pane

import (
	"strings"
	"testing"

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
