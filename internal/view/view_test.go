package view

import "testing"

func TestSnapshotPane(t *testing.T) {
	s := Snapshot{Panes: []Pane{{PaneID: "a"}, {PaneID: "b", Agent: "codex"}}}
	if p, ok := s.Pane("b"); !ok || p.Agent != "codex" {
		t.Fatal("lookup")
	}
	if _, ok := s.Pane("z"); ok {
		t.Fatal("missing pane")
	}
}
