package pane

import (
	"strings"
	"testing"

	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

func TestHeaderRosterAndFeedShowCost(t *testing.T) {
	s := snap()
	s.Panes[0].CostUSD, s.Panes[0].BudgetUSD = 0.42, 2
	s.Panes[0].Dots[0].CostUSD = 0.31
	s.Panes[0].Dots[1].CostUnknown = true
	s.Panes[0].Feed = []state.FeedEntry{
		{Turn: 4, Dot: "security", Outcome: state.OutcomeFlagged, Detail: "JWT in localStorage · cost=$0.0123 tokens=1234/210"},
		{Turn: 4, Dot: "frontend", Outcome: state.OutcomePass, Detail: "cost=? tokens=10/2"},
	}
	m := New("w1:p1", &fakeCaller{})
	m.width = 100
	mm, _ := m.Update(SnapshotMsg(s))
	m = mm.(Model)
	m.showFeed = true
	v := m.View()
	for _, want := range []string{"budget 3/120  $0.42/2.00", "$0.31", "$0.00?", "cost=$0.0123 tokens=1234/210", "cost=? tokens=10/2"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestNoCostNoClutter(t *testing.T) {
	m := New("w1:p1", &fakeCaller{})
	mm, _ := m.Update(SnapshotMsg(snap()))
	if v := mm.(Model).View(); strings.Contains(v, "$") {
		t.Fatalf("a pane with no spend shows no dollars:\n%s", v)
	}
	if paneCost(0.5, 0, false) != "  $0.50" || dotCost(view.DotState{}) != "" {
		t.Fatal("helpers")
	}
}

func TestHeaderShowsUnknownMarkAfterTotal(t *testing.T) {
	for _, tc := range []struct {
		cost, budget float64
		want         string
	}{
		{0.42, 0, "  $0.42?"},
		{0.42, 2, "  $0.42?/2.00"},
		{0, 0, "  $0.00?"},
	} {
		if got := paneCost(tc.cost, tc.budget, true); got != tc.want {
			t.Errorf("paneCost(%v,%v,true) = %q, want %q", tc.cost, tc.budget, got, tc.want)
		}
	}
	s := snap()
	s.Panes[0].CostUSD, s.Panes[0].CostUnknown = 0.42, true
	m := New("w1:p1", &fakeCaller{})
	m.width = 100
	mm, _ := m.Update(SnapshotMsg(s))
	if v := mm.(Model).View(); !strings.Contains(v, "budget 3/120  $0.42?") {
		t.Fatalf("header missing unpriced mark:\n%s", v)
	}
}

func TestCostLinesNeverWrap(t *testing.T) {
	s := snap()
	s.Panes[0].CostUSD, s.Panes[0].BudgetUSD, s.Panes[0].CostUnknown = 123.45, 200, true
	s.Panes[0].Dots[0].CostUSD, s.Panes[0].Dots[0].CostUnknown = 12.5, true
	s.Panes[0].Feed = []state.FeedEntry{{Turn: 4, Dot: "security", Outcome: state.OutcomeFlagged, Detail: "x · cost=$0.0123 tokens=123456789/210000"}}
	m := New("w1:p1", &fakeCaller{})
	m.width, m.height = 30, 20
	mm, _ := m.Update(SnapshotMsg(s))
	m = mm.(Model)
	m.showFeed = true
	checked := 0
	for _, l := range strings.Split(m.View(), "\n") {
		// Header, roster and feed lines; cards and the footer are not cost lines.
		if !strings.Contains(l, "DOTS") && !strings.Contains(l, "queue ") && !strings.Contains(l, "flagged") {
			continue
		}
		checked++
		if n := len([]rune(stripANSI(l))); n > 30 {
			t.Errorf("line wider than the pane (%d): %q", n, l)
		}
	}
	if checked < 3 {
		t.Fatalf("expected header, a roster line and a feed line, checked %d", checked)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
