package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/queue"
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

// A dot paused while its background job waits for the one background slot
// (bgSem) does not run: the post-wait gate re-reads the pause from the
// attachment instead of trusting the job's build-time copy.
func TestDotPausedWhileQueuedDoesNotRun(t *testing.T) {
	sec, fe := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
	r := newProRig(t, proOpts{}, sec, fe)
	r.attach(t, "security", "frontend")
	open := holdAll(t, r)
	first := make(chan struct{})
	j1 := paneJob(r, sec)
	j1.done = func() { close(first) }
	r.e.spawn(func() { r.e.runBackground(j1) })
	eventually(t, "the first job holds the slot", func() bool { return r.be.n() == 1 })
	second := make(chan struct{})
	j2 := paneJob(r, fe) // built unpaused
	j2.done = func() { close(second) }
	r.e.spawn(func() { r.e.runBackground(j2) })
	eventually(t, "the second job is queued", func() bool { return queuedJobs(r) == 2 })
	if err := r.e.SetPaused(pane, "frontend", true); err != nil {
		t.Fatal(err)
	}
	open()
	<-first
	<-second
	if n := r.be.n(); n != 1 {
		t.Fatalf("%d backend calls: the dot paused while queued still ran", n)
	}
}

// A research or commit run whose dot is detached mid-run ends like one whose
// pane closed: no card, no feed line, no charge to the pane. A scheduled run
// whose dot was detached goes to the project's inbox instead.
func TestDetachedDotMidRunDeliversNothing(t *testing.T) {
	for _, kind := range []string{state.RunResearch, state.RunCommit, state.RunScheduled} {
		t.Run(kind, func(t *testing.T) {
			sec, fe := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
			r := newProRig(t, proOpts{}, sec, fe)
			r.attach(t, "security", "frontend")
			r.be.set(proResult("warn", "Session cookie lacks HttpOnly."))
			open := holdAll(t, r)
			done := make(chan struct{})
			j := paneJob(r, fe)
			j.kind, j.label = kind, kind
			switch kind {
			case state.RunResearch:
				j.deliver = r.e.addAway
			case state.RunCommit:
				j.deliver = r.e.deliverCommit
			}
			j.done = func() { close(done) }
			r.e.spawn(func() { r.e.runBackground(j) })
			eventually(t, "the run is out", func() bool { return r.be.n() == 1 })
			if err := r.e.Detach(pane, "frontend"); err != nil {
				t.Fatal(err)
			}
			open()
			<-done
			if c := r.usage.Calls(pane, "frontend"); c != 0 {
				t.Errorf("a detached dot's run was charged to the pane: %d calls", c)
			}
			for _, fe := range r.feed.Recent(pane, 50) {
				if fe.Dot == "frontend" {
					t.Errorf("a detached dot's run wrote a feed line on the pane: %+v", fe)
				}
			}
			for _, c := range r.q.All() {
				if c.PaneID == pane && c.Dot == "frontend" {
					t.Errorf("a detached dot's run made a card on the pane: %+v", c)
				}
			}
			inbox, _ := r.inbox.List(testProject)
			if want := map[bool]int{true: 1, false: 0}[kind == state.RunScheduled]; len(inbox) != want {
				t.Errorf("inbox %+v, want %d card(s)", inbox, want)
			}
		})
	}
}

// Detaching a pane's last dot ends the attachment like a closed pane: its
// pending cards are orphaned and its session usage reset, so the same pane
// id attached again starts clean.
func TestDetachingTheLastDotEndsThePane(t *testing.T) {
	r := newProRig(t, proOpts{}, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	c, err := r.q.Add(queue.Card{Dot: "away", DotName: "Away", PaneID: pane, Kind: queue.KindDigest, Severity: "warn",
		Body: "1 dot · 12 min", Mode: "queue", Parts: []queue.CardPart{{Dot: "security", DotName: "Security", Severity: "warn", Body: "old finding"}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.usage.Add(pane, "security", state.Delta{Calls: 3, CostUSD: 0.2, CostKnown: true})
	if err := r.e.Detach(pane, "security"); err != nil {
		t.Fatal(err)
	}
	r.attach(t, "security")
	for _, p := range r.q.Pending(pane) {
		if p.ID == c.ID {
			t.Fatalf("the old digest is still pending on the new attachment: %+v", p)
		}
	}
	if n := r.usage.PaneCalls(pane); n != 0 {
		t.Fatalf("the new attachment inherited %d calls", n)
	}
}

// Ruling P8-R16: a Daily.Add that fails (a lock timeout) is retried with a
// short backoff before background work is blocked until midnight. Two
// failures then a success: the run is recorded and work goes on. Every
// attempt failing: blocked, with one log line and one toast.
func TestDailyAddIsRetriedBeforeBlocking(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	var mu sync.Mutex
	failures, attempts := 0, 0
	var waits []time.Duration
	var r *proRig
	r = newProRig(t, proOpts{engine: func(e *Engine) {
		e.dailyAdd = func(slug, kind string, cost float64, now time.Time) error {
			mu.Lock()
			attempts++
			fail := failures > 0
			if fail {
				failures--
			}
			mu.Unlock()
			if fail {
				return errors.New("daily.json: lock timeout")
			}
			return r.daily.Add(slug, kind, cost, now)
		}
		e.sleep = func(d time.Duration) {
			mu.Lock()
			waits = append(waits, d)
			mu.Unlock()
		}
	}}, d)
	mu.Lock()
	failures = 2
	mu.Unlock()
	r.e.runBackground(scheduledJob(r, d))
	if day := r.today(t); day.Count("security", state.RunScheduled) != 1 {
		t.Fatalf("the run was not recorded after two failed attempts: %+v", day)
	}
	mu.Lock()
	if attempts != 3 || len(waits) != 2 || waits[0] != 100*time.Millisecond || waits[1] != 300*time.Millisecond {
		t.Fatalf("attempts %d waits %v, want 3 attempts after waits [100ms 300ms]", attempts, waits)
	}
	mu.Unlock()
	r.e.runBackground(scheduledJob(r, d))
	if r.be.n() != 2 {
		t.Fatal("a Daily.Add that succeeded on a retry blocked background work")
	}

	mu.Lock()
	failures, attempts, waits = 100, 0, nil
	mu.Unlock()
	r.e.runBackground(scheduledJob(r, d))
	mu.Lock()
	if attempts != 4 || len(waits) != 3 || waits[2] != 900*time.Millisecond {
		t.Fatalf("attempts %d waits %v, want 4 attempts after waits [100ms 300ms 900ms]", attempts, waits)
	}
	mu.Unlock()
	r.e.runBackground(scheduledJob(r, d))
	if r.be.n() != 3 {
		t.Fatal("every Daily.Add attempt failed: background work must stop until midnight")
	}
	n := 0
	for _, msg := range r.toasts() {
		if strings.Contains(msg, "could not be recorded") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want one toast for the unrecorded run, got %v", r.toasts())
	}
}

// Loop guard for commits: an inject-mode commit review of a commit made
// while the pane's last turn was started by a dot is queued, never injected
// (inject → commit → review → inject would loop); otherwise it injects.
func TestInjectCommitReviewAfterADotTurnIsQueued(t *testing.T) {
	for _, fromDot := range []bool{true, false} {
		t.Run(fmt.Sprintf("fromDot=%v", fromDot), func(t *testing.T) {
			d := commitDot("Tests")
			d.Delivery.Mode = dots.ModeInject
			r, repo := commitRig(t, d)
			tr := transcript.Turn{User: "fix the CSRF check", Assistant: "done"}
			if fromDot {
				tr.FromDots = []string{"Tests"}
			}
			r.reader().add(tr)
			r.be.set(proResult("warn", "CheckCSRF has no test."))
			proCommit(t, repo, "auth.go", "package auth\n\nfunc CheckCSRF() bool { return true }\n", "add CSRF check")
			r.checkHeads(t)
			var card queue.Card
			eventually(t, "the commit card", func() bool {
				for _, c := range r.q.All() {
					if c.Origin == state.RunCommit {
						card = c
						return true
					}
				}
				return false
			})
			want := map[bool]string{true: string(dots.ModeQueue), false: string(dots.ModeInject)}[fromDot]
			if card.Mode != want {
				t.Fatalf("card mode %q, want %q: %+v", card.Mode, want, card)
			}
		})
	}
}

// closeSpawns makes every later e.spawn refuse, as after Engine.Wait, while
// the poll loop already running keeps going.
func closeSpawns(r *proRig) {
	r.e.wmu.Lock()
	r.e.closed = true
	r.e.wmu.Unlock()
}

// A research or schedule reservation whose spawn is refused (the engine is
// shutting down) is released, never left to block the dot.
func TestRefusedSpawnReleasesTheReservation(t *testing.T) {
	t.Run("research", func(t *testing.T) {
		r := idleRig(t, researchDot("Security"))
		closeSpawns(r)
		r.clock.Add(11 * time.Minute)
		a, _ := r.atts.Get(pane)
		r.e.maybeResearch(a, r.clock.Now()) // the poll loop may get there first: either reserves once
		r.e.mu.Lock()
		ran := r.e.idle[pane].ran["security"] == r.e.idle[pane].period
		r.e.mu.Unlock()
		if !ran {
			t.Fatal("research was not reserved: the test did not reach the spawn")
		}
		eventually(t, "the reservation released", func() bool {
			r.e.mu.Lock()
			inflight := r.e.idle[pane].inflight
			r.e.mu.Unlock()
			return queuedJobs(r) == 0 && inflight == 0
		})
	})
	t.Run("schedule", func(t *testing.T) {
		r := newProRig(t, proOpts{cwd: t.TempDir()}, scheduledDot("Security", "0 11 * * *"))
		r.attach(t, "security")
		eventually(t, "roster shows the next slot", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
		closeSpawns(r)
		r.clock.Add(time.Hour + 20*time.Second)
		r.e.scheduleTick()
		r.e.mu.Lock()
		next := r.e.plan["security"]
		r.e.mu.Unlock()
		if !next.After(at(11, 0)) {
			t.Fatalf("the slot did not come due: plan %v", next)
		}
		eventually(t, "the reservation released", func() bool { return queuedJobs(r) == 0 })
	})
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
