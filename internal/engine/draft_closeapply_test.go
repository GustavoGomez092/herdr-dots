package engine

import (
	"context"
	"testing"

	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
)

// The pane closes after the patch landed but before the card was marked: the
// card still ends applied, and a later discard can't record "discarded"
// after "applied".
func TestAPaneClosingMidApplyStillMarksTheCardApplied(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	r.e.testHook = func(point string) {
		if point == "apply-applied" {
			r.e.dropPane(pane)
		}
	}
	if err := r.applyReviewed(c.ID); err != nil {
		t.Fatal(err)
	}
	r.e.testHook = nil
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusApplied {
		t.Fatalf("card %+v, want applied", got)
	}
	if err := r.e.DiscardPatch(c.ID); err == nil {
		t.Fatal("discarded an applied patch")
	}
	if m, err := r.patches.Get(c.PatchID); err != nil || m.Status != patchstore.StatusApplied {
		t.Fatalf("meta %+v %v", m, err)
	}
}

func TestDiscardRefusesAPatchAlreadyApplied(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	// what an apply interrupted between the patch landing and the card write
	// leaves: the record applied, the card orphaned
	if _, err := r.patches.Update(c.PatchID, func(m *patchstore.Meta) { m.Status = patchstore.StatusApplied }); err != nil {
		t.Fatal(err)
	}
	if err := r.q.SetStatus(c.ID, queue.StatusOrphaned); err != nil {
		t.Fatal(err)
	}
	if err := r.e.DiscardPatch(c.ID); err == nil {
		t.Fatal("discarded a patch whose record says applied")
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusOrphaned {
		t.Fatalf("card %+v", got)
	}
	if m, err := r.patches.Get(c.PatchID); err != nil || m.Status != patchstore.StatusApplied {
		t.Fatalf("meta %+v %v", m, err)
	}
	if l, _ := r.mem.Ledger(testProject); len(l) != 0 {
		for _, o := range l {
			if o.Kind == memory.OutcomeDiscarded {
				t.Fatalf("a discarded line after applied: %+v", l)
			}
		}
	}
}
