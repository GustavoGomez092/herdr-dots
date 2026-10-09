package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// Phase 8 residuals: regression tests for the leftovers of the final review.

// A run the gate admits with less than a millionth of a dollar left still
// carries a cap: rounding to micro precision must not turn it into "no cap".
func TestBackgroundRunCapNeverRoundsToNoCap(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{cfg: func(c *config.Config) { c.DailyBudgetUSD = 0.0000004 }}, d) // less than a micro-dollar
	r.attach(t, "security")

	j := paneJob(r, d)
	j.kind, j.label, j.deliver = state.RunResearch, "research", nil
	n := r.be.n()
	r.e.runBackground(j)
	if r.be.n() != n+1 {
		t.Fatalf("the gate should admit a run with budget left; backend calls %d -> %d", n, r.be.n())
	}
	if got := r.be.last().MaxBudgetUSD; got < 1e-6 {
		t.Fatalf("sub-micro budget left: MaxBudgetUSD = %v, want at least 1e-6", got)
	}
}

// A turn review that has finished and paid is not also counted as in
// flight: with a session budget of two calls, one paid review leaves room
// for another dot's ask while the review is still routing its result.
func TestFinishedPaidReviewIsNotDoubleCounted(t *testing.T) {
	sec, fe := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := sync.OnceFunc(func() { close(release) })
	r := newProRig(t, proOpts{
		cfg: func(c *config.Config) { c.SessionBudgetCalls = 2 },
		hook: func(point string) {
			if point == "run-charged" {
				once.Do(func() { close(reached); <-release })
			}
		},
	}, sec, fe)
	t.Cleanup(unblock) // before the engine drains
	r.attach(t, "security")
	r.turnEnds(transcript.Turn{User: "add login", Assistant: "done"})
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the review never reached its charge")
	}
	r.attach(t, "security", "frontend")
	// Admitted: it is refused with ErrBudget at once or, admitted, waits for
	// the run slot the paid review still holds until the context ends.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := r.e.Ask(ctx, pane, "frontend", "q", false); errors.Is(err, ErrBudget) {
		t.Fatalf("an ask next to a review that already paid was refused: %v", err)
	}
}

// A council's preflight counts the pane's in-flight work as calls already
// made: with a budget of three and one ask running, two members plus the
// chair no longer fit.
func TestCouncilPreflightCountsInFlightWork(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.SessionBudgetCalls = 3 },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Docs", dots.ModeQueue))
	seat(t, r, "security", "frontend", "docs")
	cb.on("Docs", step{hang: true})
	ctx, cancel := context.WithCancel(context.Background())
	asked := make(chan error, 1)
	go func() { _, err := r.e.Ask(ctx, pane, "docs", "q", false); asked <- err }()
	t.Cleanup(func() { cancel(); <-asked })
	eventually(t, "the ask is in flight", func() bool { return cb.running() == 1 })

	n := cb.total()
	if _, err := r.e.Council(context.Background(), pane, "q", []string{"security", "frontend"}); err == nil {
		t.Fatal("2 members + chair = 3 calls with 1 already in flight and a budget of 3: the preflight must refuse")
	}
	if cb.total() != n {
		t.Fatal("a refused council must run nothing")
	}
}

// Detaching the last dot ends the pane's in-flight ask as closing the pane
// does: the ask returns ErrPaneClosed and charges nothing.
func TestDetachingTheLastDotCancelsItsInFlightAsk(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue))
	seat(t, r, "security")
	cb.on("Security", step{hang: true})
	asked := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := r.e.Ask(ctx, pane, "security", "q", false)
		asked <- err
	}()
	eventually(t, "the ask is in flight", func() bool { return cb.running() == 1 })

	if err := r.e.Detach(pane, "security"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-asked:
		if !errors.Is(err, ErrPaneClosed) {
			t.Fatalf("ask after the last dot detached: %v, want ErrPaneClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the ask kept running after the last dot detached")
	}
	if n := r.usage.PaneCalls(pane); n != 0 {
		t.Fatalf("a cancelled ask charged %d call(s)", n)
	}
}
