package engine

import (
	"context"
	"testing"

	"herdr-dots/internal/dots"
)

// Detaching the last dot decides "the attachment is empty" inside the e.mu
// section that deletes it: an Attach of another dot landing between the
// store update and that section is kept, not deleted with it.
func TestDetachKeepsADotAttachedMeanwhile(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue))
	r.attach(t, "security")
	var attachErr error
	r.e.testHook = func(point string) {
		if point == "detach-updated" {
			r.e.testHook = nil
			attachErr = r.e.Attach(context.Background(), pane, []string{"frontend"})
		}
	}
	if err := r.e.Detach(pane, "security"); err != nil {
		t.Fatal(err)
	}
	if attachErr != nil {
		t.Fatal(attachErr)
	}
	a, ok := r.atts.Get(pane)
	if !ok || a.Find("frontend") == nil {
		t.Fatalf("the dot attached meanwhile was deleted with the attachment: %+v %v", a, ok)
	}
	if a.Find("security") != nil {
		t.Fatalf("security is still attached: %+v", a.Dots)
	}
}
