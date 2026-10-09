package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
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

// holdAll blocks every backend run until the returned func is called (at
// the latest when the test ends, before the engine is drained).
func holdAll(t *testing.T, r *proRig) (open func()) {
	gate := r.be.hold("", true)
	var once sync.Once
	open = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)
	return open
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

// Ruling P8-R8, completed: a turn review (or memory pass) running on a pane
// counts as a call in flight for every session-budget check: a background
// run and an ask of another dot are refused while it holds a 1-call budget.
func TestARunningTurnReviewCountsAsInFlight(t *testing.T) {
	sec, fe := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
	fe.Trigger.OnTurn = false // only Security reviews the turn
	r := newProRig(t, proOpts{cfg: func(c *config.Config) { c.SessionBudgetCalls = 1 }}, sec, fe)
	r.attach(t, "security", "frontend")
	open := holdAll(t, r)
	r.turnEnds(transcript.Turn{User: "add login", Assistant: "done"})
	eventually(t, "the turn review is out", func() bool { return r.be.n() == 1 })

	if err := proAsk(r, "frontend"); !errors.Is(err, ErrBudget) {
		t.Errorf("ask during a running turn review over a 1-call budget: %v, want ErrBudget", err)
	}
	done := make(chan struct{})
	j := paneJob(r, fe)
	j.done = func() { close(done) }
	r.e.spawn(func() { r.e.runBackground(j) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		open()
		t.Fatal("a background run started next to a running turn review over a 1-call budget")
	}
	if n := r.be.n(); n != 1 {
		t.Fatalf("%d backend calls, want only the turn review", n)
	}
}

// The other way round: turn reviews use the same in-flight count, so an ask
// of another dot in flight over a 1-call budget stops the turn review.
func TestTurnReviewCountsAnAskInFlight(t *testing.T) {
	sec, fe := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
	fe.Trigger.OnTurn = false
	r := newProRig(t, proOpts{cfg: func(c *config.Config) { c.SessionBudgetCalls = 1 }}, sec, fe)
	r.attach(t, "security", "frontend")
	open := holdAll(t, r)
	asked := make(chan error, 1)
	go func() { asked <- proAsk(r, "frontend") }()
	eventually(t, "the ask is out", func() bool { return r.be.n() == 1 })
	r.turnEnds(transcript.Turn{User: "add login", Assistant: "done"})
	eventually(t, "the turn review is refused for the session budget", func() bool {
		return r.dotState("security").Status == view.StatusBudget
	})
	open()
	if err := <-asked; err != nil {
		t.Fatal(err)
	}
	if n := r.be.n(); n != 1 {
		t.Fatalf("%d backend calls, want only the ask", n)
	}
}
