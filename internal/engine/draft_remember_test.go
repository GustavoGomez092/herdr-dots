package engine

import (
	"context"
	"errors"
	"testing"

	"herdr-dots/internal/memory"
)

// A patch card's body is a model's summary of a diff, not advice: it is never
// remembered, whatever the client (the pane already refuses it).
func TestRememberRefusesAPatchCard(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	for _, as := range []memory.RememberAs{memory.AsFinding, memory.AsRule, memory.AsConvention, memory.AsGlobal} {
		if err := r.e.Remember(c.ID, as, ""); !errors.Is(err, ErrPatchNotRemembered) {
			t.Fatalf("Remember as %s: %v, want ErrPatchNotRemembered", as, err)
		}
	}
	if es, _ := r.mem.List(testProject, memory.Filter{}); len(es) != 0 {
		t.Fatalf("entries written: %+v", es)
	}
}
