package summon

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/council"
)

// Final review item 4: a chair that can't run (no model for its backend) is
// left out of the estimate and shown as unavailable.
func TestCouncilEstimateShowsAnUnavailableChair(t *testing.T) {
	in := council.PlanInput{Candidates: []council.Candidate{{Slug: "security", CallsLeft: 40}, {Slug: "frontend", CallsLeft: 40}},
		Rounds: 1, MaxMembers: 8, PaneLeft: 50, Chair: true, ChairUnavailable: "set [council] chair_model"}
	pf := council.Plan(in)
	if pf.Chair || pf.EstimatedCalls != 2 || pf.ChairUnavailable == "" {
		t.Fatalf("preflight %+v", pf)
	}
	want := "estimate: 2 calls (2 dots × 1 round) · 50 left · chair unavailable (set [council] chair_model)"
	if got := estimateLine(pf); got != want {
		t.Fatalf("estimate %q, want %q", got, want)
	}
}

// Phase 4: the council estimate shows the pane's dollar spend when a pane
// dollar budget is set, and a pane past it refuses the council.
func TestCouncilEstimateShowsDollarSpend(t *testing.T) {
	in := council.PlanInput{Candidates: []council.Candidate{{Slug: "security", CallsLeft: 40}, {Slug: "frontend", CallsLeft: 40}},
		Rounds: 1, MaxMembers: 8, PaneLeft: 50, Chair: true, PaneCostUSD: 0.42, PaneBudgetUSD: 2}
	if got, want := estimateLine(council.Plan(in)), "estimate: 3 calls (2 dots × 1 round + chair) · 50 left · $0.42 of $2.00 spent"; got != want {
		t.Fatalf("estimate %q, want %q", got, want)
	}
	in.PaneBudgetUSD = 0
	if got := estimateLine(council.Plan(in)); strings.Contains(got, "$") {
		t.Fatalf("no dollar budget, no dollars: %q", got)
	}
	in.PaneCostUSD, in.PaneBudgetUSD = 2.5, 2
	pf := council.Plan(in)
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(&pf))
	if !strings.Contains(m.View(), "pane dollar budget reached: $2.50 of $2.00 spent") {
		t.Fatal(m.View())
	}
	if _, _ = run(m, "enter"); len(fc.calls) != 0 {
		t.Fatalf("a refused council must not be sent: %v", fc.calls)
	}
}
