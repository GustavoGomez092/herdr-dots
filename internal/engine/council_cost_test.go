package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
)

// pricedCouncil is councilBackend with a known price on every answer, as an
// API backend reports it.
type pricedCouncil struct {
	*councilBackend
	usd float64
}

func (p pricedCouncil) Run(ctx context.Context, r backend.Request) (backend.Result, error) {
	res, err := p.councilBackend.Run(ctx, r)
	res.CostUSD, res.CostKnown = p.usd, true
	return res, err
}

// Phase 4 with Phase 3: a member past its dollar budget sits out, and a pane
// past its dollar budget refuses the council.
func TestCouncilPreflightCountsDollars(t *testing.T) {
	sec := member("Security", dots.ModeQueue)
	sec.Budget.MaxUSDPerSession = 0.5
	r, cb := councilRig(t, func(c *config.Config) { c.SessionBudgetUSD = 5 }, sec, member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	_ = r.usage.Add(pane, "security", state.Delta{Calls: 1, CostUSD: 0.6, CostKnown: true})
	pf, err := r.e.CouncilPreflight(pane, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pf.Members, ",") != "frontend" || pf.Reasons["security"] != council.ReasonBudget || !pf.Allowed ||
		pf.PaneCostUSD != 0.6 || pf.PaneBudgetUSD != 5 {
		t.Fatalf("preflight %+v: a member past its dollar budget is skipped", pf)
	}
	_ = r.usage.Add(pane, "frontend", state.Delta{Calls: 1, CostUSD: 4.5, CostKnown: true})
	const why = "pane dollar budget reached: $5.10 of $5.00 spent"
	if _, err := r.e.Council(context.Background(), pane, "", nil); err == nil || err.Error() != why {
		t.Fatalf("council: %v", err)
	}
	if cb.total() != 0 {
		t.Fatalf("a refused council spends nothing, got %d calls", cb.total())
	}
}

// The chair's spend is recorded under the council slug, so it counts toward
// the pane's dollar budget like its calls count toward the call budget.
func TestCouncilChairDollarsCountAgainstThePane(t *testing.T) {
	cb := &councilBackend{script: map[string][]step{}}
	r := newRigWith(t, func(d *Deps) {
		d.Backend = func(string) (backend.Backend, error) { return pricedCouncil{cb, 0.25}, nil }
	}, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("warn", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	cb.on(council.ChairName, says("warn", goodDigest))
	convene(t, r, "")
	councilCard(t, r)
	councilDone(t, r)
	if got := r.usage.Cost(pane, "council"); got != 0.25 {
		t.Fatalf("chair cost %v", got)
	}
	if got := r.usage.PaneCost(pane); got != 0.75 {
		t.Fatalf("pane cost %v: members and chair", got)
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.CostUSD != 0.75 {
		t.Fatalf("snapshot pane cost %v", p.CostUSD)
	}
}

// Key material in a member's or the chair's error never reaches the card,
// the feed or the engine log.
func TestCouncilErrorsAreRedacted(t *testing.T) {
	const leak = "sk-ant-api03-LEAKEDLEAKED123"
	cb := &councilBackend{script: map[string][]step{}}
	var logMu sync.Mutex
	var logs []string
	r := newRigWith(t, func(d *Deps) {
		d.Backend = func(string) (backend.Backend, error) { return cb, nil }
		d.Logf = func(f string, a ...any) { logMu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); logMu.Unlock() }
	}, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	cb.on("Security", says("warn", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	cb.on("Copy", step{err: errors.New("401 invalid x-api-key " + leak)})
	cb.on(council.ChairName, step{err: errors.New("401 invalid x-api-key " + leak)})
	convene(t, r, "")
	c := councilCard(t, r)
	councilDone(t, r)
	if s := fmt.Sprintf("%+v", c); strings.Contains(s, "LEAKED") {
		t.Fatalf("key on the card: %s", s)
	}
	if !strings.Contains(c.Note, "merged without chair") || !strings.Contains(c.Parts[2].Body, "error:") {
		t.Fatalf("card %+v", c)
	}
	if s := fmt.Sprintf("%+v", r.e.Snapshot()); strings.Contains(s, "LEAKED") {
		t.Fatal("key in a snapshot")
	}
	logMu.Lock()
	defer logMu.Unlock()
	for _, l := range logs {
		if strings.Contains(l, "LEAKED") {
			t.Fatalf("key in the engine log: %s", l)
		}
	}
}

// default_model belongs to default_backend: an API chair without chair_model
// gets its own kind's default, never "sonnet".
func TestAPIChairGetsItsKindsDefaultModel(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.Council.ChairBackend = dots.KindAnthropicAPI },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("warn", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	cb.on(council.ChairName, says("warn", goodDigest))
	convene(t, r, "")
	councilCard(t, r)
	councilDone(t, r)
	if ch := cb.callsFor(council.ChairName); len(ch) != 1 || ch[0].Model != dots.DefaultAnthropicModel {
		t.Fatalf("chair requests %+v", ch)
	}
}
