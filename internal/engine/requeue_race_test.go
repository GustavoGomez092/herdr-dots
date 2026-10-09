package engine

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/queue"
)

// raceRig stages the race Task 4 exposed: a poll's rescan sees a trust
// change first, installs it, and is still on its way to requeueAuto when
// the Trust or Untrust call that made the change returns. The trust call's
// own scan then sees nothing new, so only a direct requeue puts the waiting
// inject cards back in the queue by the time the call returns.
type raceRig struct {
	*rig
	mu       sync.Mutex
	readsIn  int    // fire at the readsIn-th scan-read from now (0 = off)
	poll     func() // what runs there: the poll's scan
	blockReq bool   // the next requeueAuto waits for release
	entered  chan struct{}
	release  chan struct{}
	polled   sync.WaitGroup // the poll's scan goroutine
}

func newRaceRig(t *testing.T, opt func(*Deps)) *raceRig {
	t.Helper()
	rr := &raceRig{entered: make(chan struct{}), release: make(chan struct{})}
	rr.rig = newRigFull(t, func(d *Deps) { opt(d); noPoll(d) }, func(e *Engine) {
		e.testHook = func(point string) {
			rr.mu.Lock()
			var run func()
			block := false
			switch point {
			case "scan-read":
				if rr.readsIn > 0 {
					if rr.readsIn--; rr.readsIn == 0 {
						run = rr.poll
					}
				}
			case "requeue":
				block, rr.blockReq = rr.blockReq, false
			}
			rr.mu.Unlock()
			if run != nil {
				run()
			}
			if block {
				close(rr.entered)
				<-rr.release
			}
		}
	})
	t.Cleanup(func() { // before the engine stops and the temp dirs go
		close(rr.release)
		rr.polled.Wait()
	})
	return rr
}

// pollWinsAt arms the race: at the n-th scan-read from now (inside the
// trust call's rescan, after it read the new trust state), a poll's scan of
// pr runs to the end of its install and then waits in requeueAuto.
func (rr *raceRig) pollWinsAt(t *testing.T, n int, pr memory.Project) {
	t.Helper()
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.readsIn = n
	rr.poll = func() {
		rr.mu.Lock()
		rr.blockReq = true
		rr.mu.Unlock()
		rr.polled.Add(1)
		go func() { defer rr.polled.Done(); rr.e.scanRoot(pr) }()
		<-rr.entered // installed; its requeue waits
	}
}

func (rr *raceRig) waitingAuto(t *testing.T, paneID string) string {
	t.Helper()
	c, err := rr.q.Add(queue.Card{PaneID: paneID, Dot: helperSlug, Mode: "inject", Status: queue.StatusAuto, Body: "do this"})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func (rr *raceRig) status(id string) queue.Status {
	c, _ := rr.q.Get(id)
	return c.Status
}

func busyPane(id, cwd, session string) herdr.PaneInfo {
	return herdr.PaneInfo{PaneID: id, Agent: "claude", AgentStatus: herdr.StatusWorking, Cwd: cwd,
		AgentSession: &herdr.AgentSession{Kind: "id", Value: session}}
}

// Carry-over (Task 4 review): re-trusting without the inject grant puts the
// dot's waiting inject cards back in the queue before Trust returns, even
// when a poll's scan installed the narrowed grant first.
func TestTrustDroppingInjectRequeuesWhenAPollScannedFirst(t *testing.T) {
	root := projectRepo(t, repoHelperTOML)
	ts := &projectdots.TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	rr := newRaceRig(t, func(d *Deps) { projectDeps(d, ts, root) })
	rr.srv.SetPane(busyPane(pane, "/repo", "s1")) // nothing flushes meanwhile
	pr := &projRig{rig: rr.rig, root: root, trust: ts}
	if err := rr.e.ScanPane(context.Background(), pane); err != nil {
		t.Fatal(err)
	}
	pr.trustAs(t, dots.CapInject)
	id := rr.waitingAuto(t, pane)
	rr.pollWinsAt(t, 1, memory.Project{ID: repoProject, Name: "app", Root: root})
	pr.trustAs(t) // the same bytes, no inject
	if st := rr.status(id); st != queue.StatusPending {
		t.Fatalf("Trust without inject returned with the waiting card %s", st)
	}
}

// Re-trusting with inject still granted leaves a waiting inject card alone.
func TestTrustKeepingInjectLeavesWaitingCards(t *testing.T) {
	r := newProjRigWith(t, noPoll)
	r.srv.SetPane(busyPane(pane, "/repo", "s1"))
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t, dots.CapInject)
	c, err := r.q.Add(queue.Card{PaneID: pane, Dot: helperSlug, Mode: "inject", Status: queue.StatusAuto, Body: "do this"})
	if err != nil {
		t.Fatal(err)
	}
	r.trustAs(t, dots.CapInject, dots.CapMCP)
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusAuto {
		t.Fatalf("a dot that still injects keeps its waiting card auto: %s", got.Status)
	}
}

// Carry-over (Task 4 review): Untrust revokes the bytes in every checkout
// that holds them; the waiting inject cards of every such checkout are back
// in the queue when it returns, not only those of the pane's checkout.
func TestUntrustRequeuesInEveryCheckoutItRescanned(t *testing.T) {
	a, b := projectRepo(t, repoHelperTOML), projectRepo(t, repoHelperTOML)
	ts := &projectdots.TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	rr := newRaceRig(t, func(d *Deps) {
		d.Trust = ts
		d.ResolveRoot = func(cwd string) (memory.Project, error) {
			root := a
			if cwd == "/b" {
				root = b
			}
			return memory.Project{ID: repoProject, Name: "app", Root: root}, nil
		}
	})
	rr.srv.SetPane(busyPane(pane, "/a", "s1"))
	rr.srv.SetPane(busyPane(pane2, "/b", "s2"))
	tc := &twoCheckouts{rig: rr.rig, a: a, b: b, trust: ts}
	for _, p := range []string{pane, pane2} {
		if err := rr.e.ScanPane(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	raw := tc.entry(t, a).SHA256
	if err := rr.e.trustAt(context.Background(), pane, a, helperSlug, raw, []string{dots.CapInject}); err != nil {
		t.Fatal(err)
	}
	if !tc.entry(t, b).Trusted {
		t.Fatal("identical bytes are trusted in B too")
	}
	inA, inB := rr.waitingAuto(t, pane), rr.waitingAuto(t, pane2)
	// rescanProject scans A, then B: the poll wins B's scan.
	rr.pollWinsAt(t, 2, memory.Project{ID: repoProject, Name: "app", Root: b})
	if err := rr.e.Untrust(context.Background(), pane, helperSlug); err != nil {
		t.Fatal(err)
	}
	if st := rr.status(inA); st != queue.StatusPending {
		t.Fatalf("A's waiting card %s", st)
	}
	if st := rr.status(inB); st != queue.StatusPending {
		t.Fatalf("Untrust returned with B's waiting card %s", st)
	}
}

// A checkout whose own (different) bytes stay trusted keeps its waiting
// inject cards when another checkout's copy is untrusted.
func TestUntrustLeavesACheckoutThatStillInjects(t *testing.T) {
	bTOML := repoHelperTOML + "\n# B's own copy\n"
	r := newTwoCheckouts(t, repoHelperTOML, bTOML)
	for _, p := range []herdr.PaneInfo{busyPane(pane, "/a", "s1"), busyPane(pane2, "/b", "s2")} {
		r.srv.SetPane(p)
	}
	for _, root := range []struct{ p, root string }{{pane, r.a}, {pane2, r.b}} {
		sum := r.entry(t, root.root).SHA256
		if err := r.e.trustAt(context.Background(), root.p, root.root, helperSlug, sum, []string{dots.CapInject}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := r.q.Add(queue.Card{PaneID: pane2, Dot: helperSlug, Mode: "inject", Status: queue.StatusAuto, Body: "do this"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.e.Untrust(context.Background(), pane, helperSlug); err != nil {
		t.Fatal(err)
	}
	if !r.entry(t, r.b).Trusted {
		t.Fatal("B's own bytes stay trusted")
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusAuto {
		t.Fatalf("B still injects: its waiting card stays auto, got %s", got.Status)
	}
}
