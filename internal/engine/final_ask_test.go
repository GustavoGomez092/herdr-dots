package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// gateRuns makes the rig's runs block until the returned func is called
// (also at cleanup, so a failed assertion never leaves a run blocked).
func gateRuns(t *testing.T, r *rig) (open func()) {
	t.Helper()
	gate := make(chan struct{})
	r.be.mu.Lock()
	r.be.gate = gate
	r.be.mu.Unlock()
	var once sync.Once
	open = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)
	return open
}

// Final review I1: Claude Code sends tool calls in parallel. Three asks of a
// dot with a budget of one call run it once; the others are refused at once
// with a clear message and use no slot.
func TestConcurrentAsksOfOneDotRunItOnce(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	d.Budget.MaxCallsPerSession = 1
	r := newRig(t, d)
	r.attach(t, "security")
	open := gateRuns(t, r)
	errs := make(chan error, 3)
	for range 3 {
		go func() { _, err := mcpAsk(t, r, "security", "q", false); errs <- err }()
	}
	for range 2 {
		select {
		case err := <-errs:
			if err == nil || !strings.Contains(err.Error(), "Security is already answering") {
				t.Fatalf("a parallel ask: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("parallel asks were not refused while the first one runs")
		}
	}
	open()
	if err := <-errs; err != nil {
		t.Fatalf("the admitted ask: %v", err)
	}
	if n, calls := r.be.n(), r.usage.Calls(pane, "security"); n != 1 || calls != 1 {
		t.Fatalf("runs=%d calls=%d, want 1 and 1", n, calls)
	}
	l := r.e.limiter()
	l.mu.Lock()
	used := l.turn[pane]
	l.mu.Unlock()
	if used != 1 {
		t.Fatalf("slots used %d, want 1 (refused asks use none)", used)
	}
}

// Final review I1: an ask in flight counts against the pane's call budget,
// so asks of two dots can't jointly overspend it.
func TestInFlightAsksCountAgainstThePaneBudget(t *testing.T) {
	r := newRigWith(t, func(d *Deps) { d.Config.SessionBudgetCalls = 1 },
		dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue))
	r.attach(t, "security", "frontend")
	open := gateRuns(t, r)
	first := make(chan error, 1)
	go func() { _, err := mcpAsk(t, r, "security", "q", false); first <- err }()
	eventually(t, "the first ask runs", func() bool { return r.be.n() == 1 })
	if _, err := mcpAsk(t, r, "frontend", "q", false); !errors.Is(err, ErrBudget) {
		t.Fatalf("second dot while the first is in flight: %v", err)
	}
	open()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if n := r.be.n(); n != 1 {
		t.Fatalf("runs %d, want 1", n)
	}
}

// Final review I1: a dot reviewing a turn can't be asked at the same time.
func TestAskWhileTheDotReviewsATurnIsRefused(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	open := gateRuns(t, r)
	r.turnEnds(t, transcript.Turn{User: "add login"})
	eventually(t, "the turn review runs", func() bool { return r.be.n() == 1 })
	if _, err := mcpAsk(t, r, "security", "q", false); err == nil || !strings.Contains(err.Error(), "Security is already answering") {
		t.Fatalf("ask during a turn review: %v", err)
	}
	open()
	eventually(t, "the review ends", func() bool { return mcpDotStatus(r, "security") != view.StatusThinking })
	if n := r.be.n(); n != 1 {
		t.Fatalf("runs %d, want 1", n)
	}
}

// Final review I1, the reverse race: a turn that ends while the dot is being
// asked waits for the ask, which consults the dot, so the turn costs no
// second run.
func TestTurnEndingDuringAnAskWaitsForIt(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	open := gateRuns(t, r)
	done := make(chan error, 1)
	go func() { _, err := mcpAsk(t, r, "security", "q", false); done <- err }()
	eventually(t, "the ask runs", func() bool { return r.be.n() == 1 })
	r.turnEnds(t, transcript.Turn{User: "add login"})
	eventually(t, "the turn is noticed", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return r.e.rerun[key{pane, "security"}]
	})
	if n := r.be.n(); n != 1 {
		t.Fatalf("a turn review started during the ask: runs %d", n)
	}
	open()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	eventually(t, "the turn is seen", func() bool { return lastSeen(r, "security") == 1 })
	time.Sleep(50 * time.Millisecond)
	if n := r.be.n(); n != 1 {
		t.Fatalf("the consulted dot reviewed the turn anyway: runs %d", n)
	}
	if st := mcpDotStatus(r, "security"); st != view.StatusConsulted {
		t.Fatalf("status %q", st)
	}
}

// Final review I1 (the reverse of ruling P5-R2): a council planned while a
// member is being asked leaves that member out, "being asked", so it shows
// under "Not heard from".
func TestCouncilSkipsADotThatIsBeingAsked(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.Council.Merge = config.MergePlain },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	wait := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(wait) }) }
	t.Cleanup(release)
	cb.on("Security", step{wait: wait})
	cb.on("Frontend", says("warn", "No loading state."))
	asked := make(chan error, 1)
	go func() { _, err := mcpAsk(t, r, "security", "q", false); asked <- err }()
	eventually(t, "the ask runs", func() bool { return cb.running() == 1 })
	pf, err := r.e.CouncilPreflight(pane, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pf.Reasons["security"] != council.ReasonAsked || strings.Join(pf.Members, ",") != "frontend,copy" {
		t.Fatalf("preflight %+v", pf)
	}
	res, err := askCouncilNow(t, r, "ready?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Digest, "Not heard from") || !strings.Contains(res.Digest, "skipped: being asked") {
		t.Fatalf("digest:\n%s", res.Digest)
	}
	if n := len(cb.callsFor("Security")); n != 1 {
		t.Fatalf("Security ran %d times, want 1 (the ask only)", n)
	}
	release()
	if err := <-asked; err != nil {
		t.Fatal(err)
	}
}

// Final review M3: an ask whose run fails because the ask was cancelled ends
// with the cancellation, never as the dot's failure ("X failed: context
// canceled"), whichever of the two the ask notices first.
func TestCancelledAskNeverReadsAsTheDotFailing(t *testing.T) {
	d := member("Security", dots.ModeQueue)
	d.Budget.MaxCallsPerSession = 10000
	r, cb := councilRig(t, func(c *config.Config) { c.SessionBudgetCalls = 10000 }, d)
	seat(t, r, "security")
	cb.on("Security", step{hang: true}) // fails with the run's context error
	for i := range 100 {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := r.e.Ask(ctx, pane, "security", "q", false)
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "failed") {
			t.Fatalf("try %d: %v", i+1, err)
		}
		eventually(t, "the ask's reservation ends", func() bool {
			r.e.mu.Lock()
			defer r.e.mu.Unlock()
			return len(r.e.asking) == 0
		})
		r.e.limiter().forget(pane)
	}
}

// Final review I1: an ask that starts while a council is being set up (its
// transcript read) still keeps that dot out of the council.
func TestCouncilStartingDuringAnAskLeavesTheDotOut(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.Council.Merge = config.MergePlain },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	wait := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(wait) }) }
	t.Cleanup(release)
	cb.on("Security", step{wait: wait})
	cb.on("Frontend", says("warn", "No loading state."))
	rd := r.readers["s1"]
	rd.arm()
	type councilOut struct {
		digest string
		err    error
	}
	cc := make(chan councilOut, 1)
	go func() { res, err := askCouncilNow(t, r, "ready?"); cc <- councilOut{res.Digest, err} }()
	<-rd.entered // planned with Security eligible; now reading the transcript
	asked := make(chan error, 1)
	go func() { _, err := mcpAsk(t, r, "security", "q", false); asked <- err }()
	eventually(t, "the ask runs", func() bool { return cb.running() == 1 })
	close(rd.release)
	out := <-cc
	if out.err != nil {
		t.Fatal(out.err)
	}
	if !strings.Contains(out.digest, "skipped: being asked") {
		t.Fatalf("digest:\n%s", out.digest)
	}
	if n := len(cb.callsFor("Security")); n != 1 {
		t.Fatalf("Security ran %d times, want 1 (the ask only)", n)
	}
	release()
	if err := <-asked; err != nil {
		t.Fatal(err)
	}
}
