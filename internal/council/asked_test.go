package council

import "testing"

// Phase 5 final review: a dot the main agent is asking sits the council out.
func TestPlanSkipsADotBeingAsked(t *testing.T) {
	p := Plan(PlanInput{Rounds: 1, PaneLeft: 100, Candidates: []Candidate{
		{Slug: "security", CallsLeft: 40, Asked: true},
		{Slug: "frontend", CallsLeft: 40},
	}})
	if len(p.Members) != 1 || p.Members[0] != "frontend" || p.Reasons["security"] != ReasonAsked || !p.Allowed {
		t.Fatalf("plan %+v", p)
	}
	if ReasonAsked != "being asked" {
		t.Fatalf("reason %q", ReasonAsked)
	}
}
