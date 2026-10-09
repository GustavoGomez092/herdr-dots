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
	s.PausedAll = true
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

func TestTinySpendIsNotShownAsZero(t *testing.T) {
	if got := paneCost(0.003, 0, false); got != "  <$0.01" {
		t.Errorf("paneCost tiny = %q", got)
	}
	if got := dotCost(view.DotState{CostUSD: 0.003}); got != " <$0.01" {
		t.Errorf("dotCost tiny = %q", got)
	}
	if got := dotCost(view.DotState{CostUSD: 0.003, CostUnknown: true}); got != " <$0.01?" {
		t.Errorf("dotCost tiny unknown = %q", got)
	}
}

func longSnap() view.Snapshot {
	s := memSnap()
	s.PausedAll = true
	s.Panes[0].ProjectName = "a-rather-long-project-name"
	s.Panes[0].CostUSD, s.Panes[0].BudgetUSD, s.Panes[0].CostUnknown = 1.5, 2, true
	return s
}

func TestHeaderNeverWraps(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*view.Snapshot)
	}{
		{"cost and paused", func(*view.Snapshot) {}},
		{"no cost", func(s *view.Snapshot) { s.Panes[0].CostUSD, s.Panes[0].BudgetUSD, s.Panes[0].CostUnknown = 0, 0, false }},
		{"no cost paused", func(s *view.Snapshot) {
			s.Panes[0].CostUSD, s.Panes[0].BudgetUSD, s.Panes[0].CostUnknown = 0, 0, false
			s.PausedAll = true
		}},
	} {
		s := longSnap()
		tc.mut(&s)
		m := New("w1:p1", &fakeCaller{})
		m.width = 30
		mm, _ := m.Update(SnapshotMsg(s))
		first := strings.Split(mm.(Model).View(), "\n")[0]
		if n := len([]rune(stripANSI(first))); n > 30 {
			t.Errorf("%s: header is %d wide: %q", tc.name, n, first)
		}
	}
}

func TestNarrowHeaderKeepsCostOverProject(t *testing.T) {
	m := New("w1:p1", &fakeCaller{})
	m.width = 60
	mm, _ := m.Update(SnapshotMsg(longSnap()))
	first := stripANSI(strings.Split(mm.(Model).View(), "\n")[0])
	if !strings.Contains(first, "$1.50?/2.00") || !strings.Contains(first, "budget 3/120") {
		t.Fatalf("cost tail dropped before the project: %q", first)
	}
	if n := len([]rune(first)); n > 60 {
		t.Fatalf("header %d wide: %q", n, first)
	}
}

func TestFeedLineNeverWrapsWithLongDot(t *testing.T) {
	s := snap()
	s.Panes[0].Feed = []state.FeedEntry{
		{Turn: 123456, Dot: "a-very-long-dot-name-indeed", Outcome: state.OutcomeFlagged, Detail: "cost=$0.0123 tokens=1/2"},
		{Turn: 123456, Dot: "a-very-long-dot-name-indeed", Outcome: "error", Detail: strings.Repeat("boom ", 20)},
	}
	m := New("w1:p1", &fakeCaller{})
	m.width = 30
	mm, _ := m.Update(SnapshotMsg(s))
	m = mm.(Model)
	m.showFeed = true
	feed := false
	for _, l := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(l, "── FEED") {
			feed = true
			continue
		}
		if strings.HasPrefix(l, "j/k move") {
			break // the footer is not a feed line
		}
		if feed && strings.TrimSpace(l) != "" && len([]rune(l)) > 30 {
			t.Errorf("feed line wider than the pane: %q", l)
		}
	}
	if !feed {
		t.Fatal("no feed section")
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
