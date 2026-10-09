package engine

import (
	"context"
	"errors"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// Phase 8 final fix wave: regression tests for the final review's findings.

// costErrBackend fails every run but reports what it spent (a claude-cli
// is_error reply with total_cost_usd).
type costErrBackend struct{ res backend.Result }

func (b costErrBackend) Kind() string                { return "claude-cli" }
func (b costErrBackend) Check(context.Context) error { return nil }
func (b costErrBackend) Run(context.Context, backend.Request) (backend.Result, error) {
	return b.res, errors.New("claude error: budget exceeded")
}

// P8-R15(b): a background run carries a per-run dollar cap of the daily
// budget left, at most $0.50; a foreground turn review carries none.
func TestBackgroundRunsCarryAMaxBudget(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{}, d)
	r.attach(t, "security")

	j := paneJob(r, d)
	j.kind, j.label, j.deliver = state.RunResearch, "research", nil
	r.e.runBackground(j)
	if got := r.be.last().MaxBudgetUSD; got != 0.50 {
		t.Fatalf("research with $1.00 left: MaxBudgetUSD = %v, want 0.50", got)
	}

	_ = r.daily.Add("frontend", state.RunResearch, 0.90, r.clock.Now()) // another dot spent $0.90
	r.e.runBackground(paneJob(r, d))
	if got := r.be.last().MaxBudgetUSD; got != 0.10 {
		t.Fatalf("scheduled run with $0.10 left: MaxBudgetUSD = %v, want 0.10", got)
	}

	n := r.be.n()
	r.turnEnds(transcript.Turn{User: "add login", Assistant: "done"})
	eventually(t, "the turn review", func() bool { return r.be.n() == n+1 })
	if req := r.be.last(); req.MaxBudgetUSD != 0 {
		t.Fatalf("a foreground review has no per-run cap: %v", req.MaxBudgetUSD)
	}
}

// P8-R15(a), end to end: a failed run that reports a cost is charged to the
// day and to the pane.
func TestFailedBackgroundRunWithACostIsCharged(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{deps: func(dp *Deps) {
		dp.Backend = func(string) (backend.Backend, error) {
			return costErrBackend{backend.Result{CostUSD: 0.42, CostKnown: true, InputTokens: 9}}, nil
		}
	}}, d)
	r.attach(t, "security")
	r.e.runBackground(paneJob(r, d))
	if day := r.today(t); day.CostUSD != 0.42 {
		t.Fatalf("daily cost %v, want 0.42", day.CostUSD)
	}
	if c := r.usage.PaneCost(pane); c != 0.42 {
		t.Fatalf("pane cost %v, want 0.42", c)
	}
}
