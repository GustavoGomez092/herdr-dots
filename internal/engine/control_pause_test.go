package engine

import (
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// E18: resuming a dot whose review is still running shows it thinking, not
// idle.
func TestResumeDuringARunShowsThinking(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.be.gate = make(chan struct{})
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "x"})
	eventually(t, "the review is in flight", func() bool { return r.be.n() == 1 })
	if err := r.e.SetPaused(pane, "security", true); err != nil {
		t.Fatal(err)
	}
	if err := r.e.SetPaused(pane, "security", false); err != nil {
		t.Fatal(err)
	}
	p, _ := r.e.Snapshot().Pane(pane)
	close(r.be.gate)
	if got := p.Dots[0].Status; got != view.StatusThinking {
		t.Fatalf("status after resume = %q, want %q", got, view.StatusThinking)
	}
}
