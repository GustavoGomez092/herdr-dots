package engine

import (
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/transcript"
)

// E33: a review that ends after its pane closed leaves no status behind, so
// a reused pane id doesn't inherit it.
func TestRunEndingAfterPaneCloseLeavesNoStatus(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.be.gate = make(chan struct{})
	r.be.set(critical)
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "x"})
	eventually(t, "the review is in flight", func() bool { return r.be.n() == 1 })
	r.srv.RemovePane(pane)
	eventually(t, "attachment dropped", func() bool { _, ok := r.atts.Get(pane); return !ok })
	close(r.be.gate)
	k := key{pane, "security"}
	eventually(t, "the run ended", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return !r.e.running[k]
	})
	time.Sleep(50 * time.Millisecond)
	r.e.mu.Lock()
	st, ok := r.e.status[k]
	r.e.mu.Unlock()
	if ok {
		t.Fatalf("closed pane kept status %q", st)
	}
}
