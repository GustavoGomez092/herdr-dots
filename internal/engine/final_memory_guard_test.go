package engine

import (
	"context"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/state"
)

// touchStall is a memory clock that, once armed, holds the first Touch call
// (the uses bump runDot makes after its first cancellation check) until
// released.
type touchStall struct {
	armed   atomic.Bool
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newTouchStall() *touchStall {
	return &touchStall{entered: make(chan struct{}), release: make(chan struct{})}
}

func (s *touchStall) now() time.Time {
	if s.armed.Load() && strings.Contains(string(debug.Stack()), "memory.(*Store).Touch") {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return time.Now()
}

// Final review M4: an ask cancelled after its run returned, but before its
// memory ops are applied, stores nothing.
func TestAskCancelledJustBeforeItsMemoryOpsLearnsNothing(t *testing.T) {
	stall := newTouchStall()
	var releaseOnce sync.Once
	unstall := func() { releaseOnce.Do(func() { close(stall.release) }) }
	t.Cleanup(unstall)
	r := newMemRig(t, memOpts{now: stall.now}, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	// A remembered note makes the run's memory block non-empty, so runDot
	// touches it (bumps its uses) after the run.
	if _, err := r.mem.Add(testProject, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote,
		Text: "Sessions expire after fifteen minutes.", Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	r.be.set(backend.Result{Verdict: backend.Verdict{Severity: "info"}, Remember: []backend.MemoryOp{
		{Op: "add", Scope: "dot", Kind: "note", Text: "The team prefers zod over hand-written validators."},
	}})
	gate := make(chan struct{})
	r.be.mu.Lock()
	r.be.gate = gate
	r.be.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.e.Ask(ctx, pane, "security", "q", false)
		done <- err
	}()
	eventually(t, "the run is in flight", func() bool { return r.be.n() == 1 })
	if !strings.Contains(r.be.last().Memory, "fifteen minutes") {
		t.Fatalf("the run had no memory to touch: %q", r.be.last().Memory)
	}
	stall.armed.Store(true)
	close(gate)
	select {
	case <-stall.entered: // the run passed its first check and is touching memory
	case <-time.After(3 * time.Second):
		t.Fatal("the run never touched memory")
	}
	cancel()
	<-done
	unstall()
	eventually(t, "the run ends", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return len(r.e.asking) == 0
	})
	time.Sleep(50 * time.Millisecond)
	for _, e := range r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"}) {
		if strings.Contains(e.Text, "zod") {
			t.Fatalf("a cancelled ask stored memory: %+v", e)
		}
	}
	if r.hasFeed(state.OutcomeLearned, "learned") {
		t.Fatal("a cancelled ask logged a learned line")
	}
}
