package engine

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"herdr-dots/internal/queue"
	"herdr-dots/internal/view"
)

// A burst of 1,000 changes in one second reaches watchers as at most 11
// snapshots, and the last one shows the final state.
func TestBroadcastsAreCoalescedAndTheLastStateArrives(t *testing.T) {
	r := newRig(t)
	ch, stop := r.e.Watch()
	defer stop()
	for len(ch) > 0 {
		<-ch
	}
	got := 0
	done := make(chan struct{})
	var last bool
	go func() {
		defer close(done)
		deadline := time.After(1500 * time.Millisecond)
		for {
			select {
			case s := <-ch:
				got++
				last = s.PausedAll
			case <-deadline:
				return
			}
		}
	}()
	start := time.Now()
	for i := 0; i < 1000; i++ {
		r.e.TogglePauseAll() // each toggle broadcasts
		time.Sleep(time.Until(start.Add(time.Duration(i+1) * time.Millisecond)))
	}
	<-done
	if got > 11 {
		t.Fatalf("%d broadcasts for 1,000 changes in 1s; the cap is 11", got)
	}
	if last != r.e.Snapshot().PausedAll {
		t.Fatalf("watchers ended on PausedAll=%v, the engine is at %v", last, r.e.Snapshot().PausedAll)
	}
}

// A single change after a quiet period is not delayed.
func TestFirstBroadcastAfterAQuietGapIsImmediate(t *testing.T) {
	r := newRig(t)
	ch, stop := r.e.Watch()
	defer stop()
	time.Sleep(2 * minBroadcastGap)
	for len(ch) > 0 {
		<-ch
	}
	r.e.TogglePauseAll()
	// Sent at once means sent before broadcast returns: the snapshot is
	// already in the channel. No clock bound, so a loaded host under -race
	// can't make this flaky; a coalesced (delayed) send would not be there.
	select {
	case s := <-ch:
		if !s.PausedAll {
			t.Fatal("snapshot without the change")
		}
	default:
		t.Fatal("the first broadcast after a quiet gap was not sent at once")
	}
}

// A snapshot still over budget after trimming (the floors: one card per pane,
// every library entry) is logged once per session, not on every broadcast.
func TestAnOverBudgetSnapshotIsLoggedOncePerSession(t *testing.T) {
	var mu sync.Mutex
	var over int
	r := newRigWith(t, func(d *Deps) {
		d.Logf = func(f string, a ...any) {
			if strings.Contains(fmt.Sprintf(f, a...), "over the snapshot budget") {
				mu.Lock()
				over++
				mu.Unlock()
			}
		}
	})
	r.e.Snapshot()
	if _, err := r.q.Add(queue.Card{PaneID: "w9:p9", Dot: "security", Severity: "critical", Mode: "queue",
		Body: strings.Repeat("x", view.MaxSnapshotBytes)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if p, ok := r.e.Snapshot().Pane("w9:p9"); !ok || len(p.Cards) != 1 {
			t.Fatal("the one card is the floor: it is shown")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if over != 1 {
		t.Fatalf("logged %d times; want once per session", over)
	}
}

// Two sends can overlap: an immediate one still building or trimming its
// snapshot when the trailing one fires. The older snapshot must never land on
// top of the newer one, or the UIs would show a stale state (a card already
// sent, a pause already lifted) until some unrelated change.
func TestAnOlderSnapshotNeverOverwritesANewerOne(t *testing.T) {
	var armed atomic.Bool
	built, release := make(chan struct{}), make(chan struct{})
	r := newRigFull(t, nil, func(e *Engine) {
		e.bc.built = func() {
			if armed.CompareAndSwap(true, false) {
				close(built)
				<-release
			}
		}
	})
	ch, stop := r.e.Watch()
	defer stop()
	time.Sleep(2 * minBroadcastGap)
	for len(ch) > 0 {
		<-ch
	}
	armed.Store(true)
	first := make(chan struct{})
	go func() { defer close(first); r.e.TogglePauseAll() }() // sent at once; held after building PausedAll=true
	<-built
	r.e.TogglePauseAll() // within the gap: the trailing broadcast carries PausedAll=false
	select {
	case s := <-ch:
		if s.PausedAll {
			t.Fatal("the trailing broadcast missed the second change")
		}
	case <-time.After(time.Second):
		t.Fatal("no trailing broadcast")
	}
	close(release)
	<-first
	time.Sleep(2 * minBroadcastGap)
	select {
	case s := <-ch:
		if s.PausedAll != r.e.Snapshot().PausedAll {
			t.Fatalf("watchers got the older snapshot (PausedAll=%v) after the newer one", s.PausedAll)
		}
	default:
	}
}
