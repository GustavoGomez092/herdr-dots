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

// An apply request must carry the hash of the patch the user reviewed: one
// without it (any IPC client that skipped the viewer) applies nothing.
func TestAnApplyWithoutTheReviewedHashIsRefused(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	raw, _ := json.Marshal(ipc.CardArgs{ID: c.ID})
	if strings.Contains(string(raw), "sha256") {
		t.Fatalf("the field stays omitempty for other commands: %s", raw)
	}
	err := r.e.Handle(context.Background(), ipc.CmdApplyPatch, raw)
	if err == nil || !strings.Contains(err.Error(), "open the patch to review it before applying") {
		t.Fatalf("empty hash: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.repo, "auth.js")); strings.Contains(string(got), "setCookie") {
		t.Fatal("an apply without a hash changed the working tree")
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusPending {
		t.Fatalf("card %+v", got)
	}
}

// applyReviewed applies a card as the diff viewer does: with the hash of the
// stored patch (a placeholder when there is none, so the error is the one the
// card's state gives).
func (r *draftRig) applyReviewed(cardID string) error {
	sha := "unreviewed"
	if c, ok := r.q.Get(cardID); ok {
		if m, err := r.patches.Get(c.PatchID); err == nil && m.SHA256 != "" {
			sha = m.SHA256
		}
	}
	return r.e.ApplyPatch(cardID, sha)
}
