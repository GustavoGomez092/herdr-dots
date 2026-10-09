package engine

import (
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/transcript"
)

func TestDecideMemoryStopsAtDollarBudgets(t *testing.T) {
	d := dot("Copy", dots.ModeObserve)
	turns := []transcript.Turn{{Index: 1}, {Index: 2}, {Index: 3}, {Index: 4}, {Index: 5}}
	base := MemoryInput{Dot: d, NewTurns: turns, MemoryEnabled: true, PaneBudget: 120}
	if !DecideMemory(base) {
		t.Fatal("baseline: 5 new turns with default memory settings run a pass")
	}
	in := base
	in.DotBudgetUSD, in.DotCostUSD = 0.5, 0.5
	if DecideMemory(in) {
		t.Fatal("dot dollar budget reached: no memory pass")
	}
	in = base
	in.PaneBudgetUSD, in.PaneCostUSD = 1, 1.2
	if DecideMemory(in) {
		t.Fatal("pane dollar budget reached: no memory pass")
	}
	in = base
	in.DotCostUSD, in.PaneCostUSD = 99, 99 // budgets 0 = off
	if !DecideMemory(in) {
		t.Fatal("zero budgets are off")
	}
}
