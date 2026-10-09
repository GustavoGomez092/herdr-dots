package engine

import (
	"context"
	"os"
	"strings"
	"testing"

	"herdr-dots/internal/queue"
)

// Fix 3, both orders, at the point where the other operation would slip in.
func TestApplyAndDiscardCannotInterleave(t *testing.T) {
	setup := func(t *testing.T) (*draftRig, queue.Card) {
		r := newDraftRig(t, draftOpts{}, writeDot("Security"))
		r.dr.set(fixJWT, nil)
		r.attach(t, "security")
		if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
			t.Fatal(err)
		}
		c := r.patchCard(t)
		r.noDraftsInFlight(t)
		return r, c
	}
	t.Run("an apply during a discard is refused", func(t *testing.T) {
		r, c := setup(t)
		var applyErr error
		r.e.testHook = func(point string) {
			if point == "discard-reserved" {
				applyErr = r.applyReviewed(c.ID)
			}
		}
		if err := r.e.DiscardPatch(c.ID); err != nil {
			t.Fatal(err)
		}
		if applyErr == nil || !strings.Contains(applyErr.Error(), "already being") {
			t.Fatalf("apply during discard: %v", applyErr)
		}
		if got, _ := os.ReadFile(r.repo + "/auth.js"); strings.Contains(string(got), "setCookie") {
			t.Fatal("the patch was applied")
		}
	})
	t.Run("a discard that lands before an apply reserves is seen by the apply", func(t *testing.T) {
		r, c := setup(t)
		r.e.testHook = func(point string) {
			if point == "apply-before-reserve" {
				r.e.testHook = nil
				if err := r.e.DiscardPatch(c.ID); err != nil {
					t.Error(err)
				}
			}
		}
		err := r.applyReviewed(c.ID)
		if err == nil || !strings.Contains(err.Error(), "dismissed") {
			t.Fatalf("apply after discard: %v", err)
		}
		if got, _ := os.ReadFile(r.repo + "/auth.js"); strings.Contains(string(got), "setCookie") {
			t.Fatal("the patch was applied")
		}
	})
}
