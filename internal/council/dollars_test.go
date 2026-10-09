package council

import "testing"

// Phase 4: a member past its max_usd_per_session sits out like a member out
// of calls, and a pane past session_budget_usd refuses the council, even
// after the popup narrows the selection.
func TestPlanCountsDollarBudgets(t *testing.T) {
	p := Plan(PlanInput{Candidates: []Candidate{
		{Slug: "security", CallsLeft: 40, OverUSD: true},
		{Slug: "frontend", CallsLeft: 40},
	}, Rounds: 1, PaneLeft: 100})
	if len(p.Members) != 1 || p.Members[0] != "frontend" || p.Reasons["security"] != ReasonBudget || !p.Allowed {
		t.Fatalf("member dollars: %+v", p)
	}
	in := PlanInput{Candidates: []Candidate{{Slug: "security", CallsLeft: 40}, {Slug: "frontend", CallsLeft: 40}},
		Rounds: 1, PaneLeft: 100, Chair: true, PaneCostUSD: 2.5, PaneBudgetUSD: 2}
	const why = "pane dollar budget reached: $2.50 of $2.00 spent"
	if p := Plan(in); p.Allowed || p.Refusal != why || p.PaneCostUSD != 2.5 || p.PaneBudgetUSD != 2 {
		t.Fatalf("pane dollars: %+v", p)
	}
	if p := Plan(in).Select([]string{"frontend"}); p.Allowed || p.Refusal != why {
		t.Fatalf("Select keeps the dollar refusal: %+v", p)
	}
	in.PaneBudgetUSD = 0 // off
	if p := Plan(in); !p.Allowed {
		t.Fatalf("a zero dollar budget is off: %+v", p)
	}
}
