package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
)

// P7-R7: the diff viewer sends the hash of the patch the user saw; a different
// hash applies nothing, the matching one applies.
func TestApplyPatchRefusesAPatchThatIsNotTheOneReviewed(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	m, err := r.patches.Get(c.PatchID)
	if err != nil || m.SHA256 == "" {
		t.Fatalf("meta %+v %v", m, err)
	}
	apply := func(sha string) error {
		raw, _ := json.Marshal(ipc.CardArgs{ID: c.ID, SHA256: sha})
		return r.e.Handle(context.Background(), ipc.CmdApplyPatch, raw)
	}
	other := strings.Repeat("0", len(m.SHA256))
	if err := apply(other); err == nil || !strings.Contains(err.Error(), "changed since you reviewed it") {
		t.Fatalf("mismatched hash: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.repo, "auth.js")); strings.Contains(string(got), "setCookie") {
		t.Fatal("a mismatched hash applied the patch")
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusPending {
		t.Fatalf("card %+v", got)
	}
	if err := apply(m.SHA256); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.repo, "auth.js")); !strings.Contains(string(got), "setCookie") {
		t.Fatal("the matching hash did not apply")
	}
}
