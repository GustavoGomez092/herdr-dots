package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// If the record can't learn its card, the card is taken back and the patch
// deleted, and the draft fails visibly: a card whose record doesn't name it
// would be swept as an orphan later.
func TestADraftWhoseRecordCantNameItsCardFailsVisibly(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	var patchID string
	// Set before attach: attach starts a catch-up review that reads the hook.
	r.e.testHook = func(point string) {
		if point != "draft-carded" {
			return
		}
		// the record turns unreadable: Update fails, Delete still works
		for _, c := range r.q.All() {
			if c.Kind == queue.KindPatch {
				patchID = c.PatchID
			}
		}
		meta := filepath.Join(r.patches.Dir, patchID+".json")
		if err := os.Remove(meta); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(meta, 0o700); err != nil {
			t.Error(err)
		}
	}
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	r.draftsDone(t)
	eventually(t, "a visible failure", func() bool { return r.feedHasDraft(state.OutcomeError, "draft failed") })
	r.e.testHook = nil
	if patchID == "" {
		t.Fatal("no card was ever added")
	}
	for _, c := range r.q.All() {
		if c.Kind == queue.KindPatch && c.Status != queue.StatusDismissed {
			t.Fatalf("card left: %+v", c)
		}
	}
	if _, err := os.Stat(r.patches.PatchPath(patchID)); !os.IsNotExist(err) {
		t.Fatal("patch file left")
	}
	if _, err := os.Stat(filepath.Join(r.patches.Dir, patchID+".json")); !os.IsNotExist(err) {
		t.Fatal("record left")
	}
	if r.refs(t) != "" {
		t.Fatalf("snapshot ref left: %s", r.refs(t))
	}
}
