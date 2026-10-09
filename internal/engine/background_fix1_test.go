package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// Task 10 fix round 1.

// liveOn is the pending or auto cards on paneID.
func liveOn(r *proRig, paneID string) []queue.Card {
	var out []queue.Card
	for _, c := range r.q.All() {
		if c.PaneID == paneID && (c.Status == queue.StatusPending || c.Status == queue.StatusAuto) {
			out = append(out, c)
		}
	}
	return out
}

func orphanedOn(r *proRig, paneID string) int {
	n := 0
	for _, c := range r.q.All() {
		if c.PaneID == paneID && c.Status == queue.StatusOrphaned {
			n++
		}
	}
	return n
}

var csrf = backend.Verdict{Relevant: true, Severity: "warn", Message: "Commit 3f2a skips the CSRF check."}

// A scheduled card delivered while dropPane runs (after its old orphan step,
// before its locked section) never stays pending on the dropped pane id, nor
// on the id's next attachment: it is orphaned with the pane's other cards.
func TestDropPaneOrphansAScheduledCardDeliveredMeanwhile(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	var rp atomic.Pointer[proRig]
	var job atomic.Pointer[bgJob]
	r := newProRig(t, proOpts{hook: func(point string) {
		if point != "drop-before-lock" {
			return
		}
		if j := job.Swap(nil); j != nil {
			rp.Load().e.deliverScheduled(*j, csrf)
		}
	}}, d)
	rp.Store(r)
	r.attach(t, "security")
	j := paneJob(r, d)
	job.Store(&j)
	r.e.dropPane(pane)
	if live := liveOn(r, pane); len(live) != 0 {
		t.Fatalf("a card is pending on the dropped pane: %+v", live)
	}
	cs, _ := r.inbox.List(testProject)
	if orphanedOn(r, pane)+len(cs) != 1 {
		t.Fatalf("the paid result was lost: %d orphaned, inbox %+v", orphanedOn(r, pane), cs)
	}
	r.attach(t, "security") // the id is reused
	time.Sleep(50 * time.Millisecond)
	for _, c := range liveOn(r, pane) {
		if c.SourceID == "" {
			t.Fatalf("the reused pane inherited the old attachment's card: %+v", c)
		}
	}
}

// A pane closed (or reused) during the run's memory I/O gets no feed line:
// a commit review ends there; a scheduled result feeds no pane and goes to
// the project's inbox.
func TestPaneClosedDuringMemoryIOGetsNoFeedLine(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	var rp atomic.Pointer[proRig]
	var armed atomic.Bool
	r := newProRig(t, proOpts{hook: func(point string) {
		if point == "bg-before-feed" && armed.CompareAndSwap(true, false) {
			rp.Load().e.dropPane(pane)
		}
	}}, d)
	rp.Store(r)
	r.attach(t, "security")
	r.be.set(proResult("warn", "The new endpoint has no rate limit."))
	j := paneJob(r, d)
	j.kind, j.label = state.RunCommit, "commit a1b2c3d"
	j.deliver = func(bgJob, backend.Verdict) { t.Error("a review for a closed pane was delivered") }
	armed.Store(true)
	r.e.runBackground(j)
	if r.hasFeed(pane, state.OutcomeCommit, "commit a1b2c3d") {
		t.Fatalf("the closed pane got a feed line: %+v", r.feed.Recent(pane, 10))
	}

	r.attach(t, "security")
	s := paneJob(r, d)
	armed.Store(true)
	r.e.runBackground(s)
	if r.hasFeed(pane, state.OutcomeScheduled, "scheduled Wed 09:00") {
		t.Fatalf("the closed pane got a scheduled feed line: %+v", r.feed.Recent(pane, 10))
	}
	if cs, _ := r.inbox.List(testProject); len(cs) != 1 {
		t.Fatalf("the scheduled result does not wait in the inbox: %+v", cs)
	}
}

// claimInbox re-checks the pane after Claim: cards claimed for a pane that
// closed (or whose id now names a new attachment) meanwhile are orphaned,
// never left pending on a dead or reused pane id.
func TestClaimInboxOrphansCardsForAPaneThatClosedMeanwhile(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		d := dot("Security", dots.ModeQueue)
		var rp atomic.Pointer[proRig]
		var armed atomic.Bool
		var after atomic.Pointer[chan struct{}] // later claims wait until this one has ended
		r := newProRig(t, proOpts{hook: func(point string) {
			if point != "inbox-before-claim" {
				return
			}
			if armed.CompareAndSwap(true, false) {
				r := rp.Load()
				r.e.dropPane(pane)
				if reuse {
					if err := r.e.Attach(context.Background(), pane, []string{"security"}); err != nil {
						t.Error(err)
					}
				}
			} else if ch := after.Load(); ch != nil {
				<-*ch
			}
		}}, d)
		rp.Store(r)
		r.attach(t, "security")
		eventually(t, "the first inbox sweep", func() bool {
			r.e.mu.Lock()
			defer r.e.mu.Unlock()
			return !r.e.inboxAt.IsZero()
		})
		time.Sleep(100 * time.Millisecond) // the attach's and the sweep's claims of the empty inbox end
		if err := r.inbox.Put(queue.Card{Dot: "security", Project: testProject, Body: "x", Mode: "queue", Origin: state.RunScheduled}); err != nil {
			t.Fatal(err)
		}
		ch := make(chan struct{})
		after.Store(&ch)
		armed.Store(true)
		r.e.claimInbox(pane, testProject)
		close(ch)
		time.Sleep(50 * time.Millisecond) // the reuse attach's own claim finds an empty inbox
		if live := liveOn(r, pane); len(live) != 0 {
			t.Fatalf("reuse=%v: a claimed card is pending on the closed pane id: %+v", reuse, live)
		}
		if n := orphanedOn(r, pane); n != 1 {
			t.Fatalf("reuse=%v: %d orphaned cards, want the claimed one", reuse, n)
		}
	}
}

// A job waiting for the background slot is not in flight: it neither blocks
// an ask nor shows as running.
func TestAQueuedBackgroundJobDoesNotBlockAnAsk(t *testing.T) {
	sec, fe, other := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue), dot("Docs", dots.ModeQueue)
	r := newProRig(t, proOpts{cfg: func(c *config.Config) { c.SessionBudgetCalls = 1 }}, sec, fe, other)
	r.attach(t, "security", "frontend")
	gate := r.be.hold(backend.PassResearch, false)
	running := make(chan struct{})
	research := scheduledJob(r, other) // pane-less: it holds the slot
	research.kind, research.label = state.RunResearch, "research"
	research.done = func() { close(running) }
	r.e.spawn(func() { r.e.runBackground(research) })
	eventually(t, "the research run is out", func() bool { return r.be.n() == 1 })
	queued := make(chan struct{})
	j := paneJob(r, sec)
	j.done = func() { close(queued) }
	r.e.spawn(func() { r.e.runBackground(j) })
	time.Sleep(50 * time.Millisecond) // it waits for the slot
	if s := r.dotState("security"); s.Background != "" {
		t.Errorf("a queued job shows as running: %q", s.Background)
	}
	if err := proAsk(r, "frontend"); err != nil {
		t.Errorf("an ask next to a queued background job: %v", err)
	}
	close(gate)
	<-running
	<-queued
}

// The roster's background label comes from the job that is running, not
// from one queued behind it on the same pane and dot.
func TestBackgroundLabelIsTheRunningJobs(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{}, d)
	r.attach(t, "security")
	gate := r.be.hold("", true)
	commit := paneJob(r, d)
	commit.kind, commit.label = state.RunCommit, "commit a1b2c3d"
	r.e.spawn(func() { r.e.runBackground(commit) })
	eventually(t, "the commit review is out", func() bool { return r.be.n() == 1 })
	r.e.spawn(func() { r.e.runBackground(paneJob(r, d)) })
	time.Sleep(50 * time.Millisecond) // the scheduled run queues behind it
	if s := r.dotState("security"); s.Background != state.RunCommit {
		t.Errorf("label %q while the commit review runs", s.Background)
	}
	close(gate)
	eventually(t, "both ran and ended", func() bool { return r.be.n() == 2 && r.dotState("security").Background == "" })
}
