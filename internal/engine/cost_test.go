package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// costBE answers every run with a fixed priced result (or err).
type costBE struct {
	mu   sync.Mutex
	res  backend.Result
	err  error
	reqs []backend.Request
}

func (c *costBE) Kind() string                { return "anthropic-api" }
func (c *costBE) Check(context.Context) error { return nil }
func (c *costBE) Run(_ context.Context, r backend.Request) (backend.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reqs = append(c.reqs, r)
	return c.res, c.err
}
func (c *costBE) n() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.reqs) }

func costRig(t *testing.T, be *costBE, cfgUSD float64, ds ...dots.Dot) *rig {
	t.Helper()
	return newRigWith(t, func(d *Deps) {
		d.Backend = func(string) (backend.Backend, error) { return be, nil }
		d.Config.SessionBudgetUSD = cfgUSD
	}, ds...)
}

func apiDot(name string) dots.Dot {
	d := dot(name, dots.ModeQueue)
	d.Backend = dots.BackendSpec{Kind: "anthropic-api", Model: "claude-opus-5-5", Args: []string{},
		Effort: "low", MaxTokens: 2000, Fallbacks: "off"}
	return d
}

func TestDecideDollarBudgets(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*FilterInput)
		want Reason
	}{
		{"dot dollars reached", func(in *FilterInput) { in.DotBudgetUSD, in.DotCostUSD = 0.5, 0.5 }, ReasonBudget},
		{"dot dollars under", func(in *FilterInput) { in.DotBudgetUSD, in.DotCostUSD = 0.5, 0.49 }, ReasonRun},
		{"pane dollars reached", func(in *FilterInput) { in.PaneBudgetUSD, in.PaneCostUSD = 2, 2.01 }, ReasonSessionBudget},
		{"zero budget is off", func(in *FilterInput) { in.DotCostUSD, in.PaneCostUSD = 100, 100 }, ReasonRun},
	}
	for _, c := range cases {
		in := baseInput()
		c.mod(&in)
		if got := Decide(in); got.Reason != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got.Reason, c.want)
		}
	}
}

func TestRunDotSendsAPIFieldsAndRecordsDollars(t *testing.T) {
	be := &costBE{res: backend.Result{Verdict: backend.Verdict{Severity: "info"}, InputTokens: 1200, OutputTokens: 80,
		CacheReadTokens: 900, CostUSD: 0.0123, CostKnown: true, Model: "claude-opus-4-8", ToolCalls: 8, ToolsCapped: true}}
	r := costRig(t, be, 0, apiDot("Security"))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "add login"})
	eventually(t, "a run", func() bool { return be.n() == 1 })
	req := be.reqs[0]
	if req.Effort != "low" || req.MaxTokens != 2000 || req.Fallbacks != "off" || req.Model != "claude-opus-5-5" || req.Cwd != "/repo" {
		t.Fatalf("request %+v", req)
	}
	eventually(t, "cost recorded", func() bool { return r.usage.Cost(pane, "security") == 0.0123 })
	eventually(t, "feed has cost", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		for _, f := range p.Feed {
			if f.Outcome == state.OutcomePass && f.Detail == "cost=$0.0123 tokens=1200/80 tools=8 (cap) model=claude-opus-4-8" {
				return true
			}
		}
		return false
	})
	p, _ := r.e.Snapshot().Pane(pane)
	if p.CostUSD != 0.0123 || p.Dots[0].CostUSD != 0.0123 || p.Dots[0].CostUnknown {
		t.Fatalf("snapshot %+v", p)
	}
}

func TestUnpricedChairRunMarksThePaneCostUnknown(t *testing.T) {
	be := &costBE{res: backend.Result{Verdict: backend.Verdict{Severity: "info"}}}
	r := costRig(t, be, 0, apiDot("Local"))
	r.attach(t, "local")
	_ = r.usage.Add(pane, "council", state.Delta{Calls: 1}) // an unpriced chair run
	p, _ := r.e.Snapshot().Pane(pane)
	if !p.CostUnknown || p.Dots[0].CostUnknown {
		t.Fatalf("pane %v dot %v: the pane flag must carry the chair's unknown cost", p.CostUnknown, p.Dots[0].CostUnknown)
	}
}

func TestDollarBudgetStopsTheDot(t *testing.T) {
	be := &costBE{res: backend.Result{Verdict: backend.Verdict{Severity: "info"}, CostUSD: 0.30, CostKnown: true}}
	d := apiDot("Security")
	d.Budget.MaxUSDPerSession = 0.50
	r := costRig(t, be, 0, d)
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "one"})
	eventually(t, "first run", func() bool { return be.n() == 1 })
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "second run", func() bool { return be.n() == 2 })
	eventually(t, "spend 0.60", func() bool { return r.usage.Cost(pane, "security") > 0.59 })
	r.turnEnds(t, transcript.Turn{User: "three"})
	eventually(t, "budget status", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Dots) == 1 && p.Dots[0].Status == view.StatusBudget
	})
	if be.n() != 2 {
		t.Fatalf("runs = %d: the dollar budget must stop the third", be.n())
	}
}

func TestPaneDollarBudgetFromConfig(t *testing.T) {
	be := &costBE{res: backend.Result{Verdict: backend.Verdict{Severity: "info"}, CostUSD: 1.5, CostKnown: true}}
	r := costRig(t, be, 1.0, apiDot("Security"))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "one"})
	eventually(t, "first run", func() bool { return be.n() == 1 })
	eventually(t, "spend", func() bool { return r.usage.PaneCost(pane) == 1.5 })
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "budget status", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.BudgetUSD == 1.0 && len(p.Dots) == 1 && p.Dots[0].Status == view.StatusBudget
	})
}

func TestUnknownCostNeverCountsTowardDollarBudgets(t *testing.T) {
	// A real unpriced run has tokens (one with none never reached the
	// provider and costs a known $0: final review item 8).
	be := &costBE{res: backend.Result{Verdict: backend.Verdict{Severity: "info"}, InputTokens: 100, OutputTokens: 5, CostUSD: 0, CostKnown: false}}
	d := apiDot("Local")
	d.Budget.MaxUSDPerSession = 0.01
	r := costRig(t, be, 0.01, d)
	r.attach(t, "local")
	for i := 0; i < 3; i++ {
		r.turnEnds(t, transcript.Turn{User: "x"})
		eventually(t, "run", func() bool { return be.n() == i+1 })
	}
	p, _ := r.e.Snapshot().Pane(pane)
	if !p.Dots[0].CostUnknown || p.CostUSD != 0 {
		t.Fatalf("snapshot %+v", p.Dots[0])
	}
}

func TestRunErrorsReachTheFeedRedacted(t *testing.T) {
	be := &costBE{err: errors.New("upstream said: invalid key sk-ant-api03-LEAKEDLEAKED123")}
	var logMu sync.Mutex
	var logs []string
	r := newRigWith(t, func(d *Deps) {
		d.Backend = func(string) (backend.Backend, error) { return be, nil }
		d.Logf = func(f string, a ...any) { logMu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); logMu.Unlock() }
	}, apiDot("Security"))
	r.attach(t, "security")
	defer func() {
		logMu.Lock()
		defer logMu.Unlock()
		for _, l := range logs {
			if strings.Contains(l, "LEAKED") {
				t.Errorf("key leaked into the engine log: %s", l)
			}
		}
		if s := fmt.Sprintf("%+v", r.e.Snapshot()); strings.Contains(s, "LEAKED") {
			t.Errorf("key leaked into a snapshot")
		}
	}()
	r.turnEnds(t, transcript.Turn{User: "x"})
	eventually(t, "error in feed", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		for _, f := range p.Feed {
			if f.Outcome == state.OutcomeError {
				if strings.Contains(f.Detail, "LEAKED") {
					t.Fatalf("key leaked into the feed: %s", f.Detail)
				}
				return true
			}
		}
		return false
	})
}
