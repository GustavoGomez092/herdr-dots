package engine

import (
	"context"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
)

// A Detach of the last dot that lands while a second Attach reads the
// transcript must stick: Attach's Upsert base comes from the live pane, not
// from the attachment it read before the Detach.
func TestAttachDoesNotResurrectDotDetachedDuringRead(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue))
	// A working agent keeps processPaneIfIdle from reading the transcript, so
	// the only Turns call after arm() is the second Attach's own read.
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusWorking, Cwd: "/repo",
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	r.attach(t, "security")
	rd := r.readers["s1"]
	rd.arm()
	done := make(chan error, 1)
	go func() { done <- r.e.Attach(context.Background(), pane, []string{"frontend"}) }()
	select {
	case <-rd.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Attach never read the transcript")
	}
	if err := r.e.Detach(pane, "security"); err != nil { // the last dot: the attachment is deleted
		t.Fatal(err)
	}
	if _, ok := r.atts.Get(pane); ok {
		t.Fatal("detaching the last dot must delete the attachment")
	}
	close(rd.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	a, ok := r.atts.Get(pane)
	if !ok || a.Find("frontend") == nil {
		t.Fatalf("frontend not attached: %+v", a.Dots)
	}
	if a.Find("security") != nil {
		t.Fatalf("security was detached during Attach and came back: %+v", a.Dots)
	}
	if a.Agent != "claude" || a.SessionValue != "s1" || a.Cwd != "/repo" {
		t.Fatalf("recreated attachment must come from the live pane: %+v", a)
	}
}
