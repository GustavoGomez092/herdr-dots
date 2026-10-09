package view

import (
	"encoding/json"
	"strings"
	"testing"

	"herdr-dots/internal/council"
)

func TestSnapshotPane(t *testing.T) {
	s := Snapshot{Panes: []Pane{{PaneID: "a"}, {PaneID: "b", Agent: "codex"}}}
	if p, ok := s.Pane("b"); !ok || p.Agent != "codex" {
		t.Fatal("lookup")
	}
	if _, ok := s.Pane("z"); ok {
		t.Fatal("missing pane")
	}
}

func TestCouncilFieldsRoundTripAsJSON(t *testing.T) {
	pf := council.Plan(council.PlanInput{Rounds: 1, PaneLeft: 10, Chair: true, Candidates: []council.Candidate{{Slug: "security", CallsLeft: 5}}})
	in := Snapshot{Panes: []Pane{{PaneID: "a", Council: &CouncilState{ID: "council-1", Phase: CouncilMembers, Done: 1, Total: 3, Round: 1}, CouncilPreflight: &pf}}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Snapshot
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	p, _ := out.Pane("a")
	if p.Council == nil || *p.Council != *in.Panes[0].Council || p.CouncilPreflight == nil || !p.CouncilPreflight.Allowed || p.CouncilPreflight.Members[0] != "security" {
		t.Fatalf("round trip: %+v", p)
	}
	if b, _ := json.Marshal(Pane{PaneID: "b"}); strings.Contains(string(b), "council") {
		t.Fatalf("no council fields when idle: %s", b)
	}
}
