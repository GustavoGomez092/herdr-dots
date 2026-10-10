package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// D3: the draft sweep removes a patch file with no record (a crash between
// PutPatch's two writes).
func TestTheDraftSweepRemovesAPatchFileWithoutARecord(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	if err := os.MkdirAll(r.patches.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := r.patches.PatchPath("0123456789abcdef")
	if err := os.WriteFile(orphan, []byte("diff --git a/x b/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.e.maintainDrafts()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the orphan patch file stayed: %v", err)
	}
}

// G12: a draft's patch text is captured up to the dot's max_patch_bytes
// only, so a huge draft is rejected as too large without being held whole.
func TestADraftOverMaxPatchBytesIsRejectedFromACappedCapture(t *testing.T) {
	d := writeDot("Security")
	d.Capabilities.MaxPatchBytes = 1000
	r := newDraftRig(t, draftOpts{}, d)
	r.attach(t, "security")
	r.dr.set(func(dir string) {
		writeT(t, filepath.Join(dir, "big.txt"), strings.Repeat("a line of text\n", 5000))
	}, nil)
	id, err := r.e.Draft(context.Background(), pane, "security", "fix", "")
	if err != nil {
		t.Fatal(err)
	}
	r.draftsDone(t)
	eventually(t, "a rejection in the feed", func() bool { return r.feedHasDraft(state.OutcomeError, "patch rejected") })
	m, err := r.patches.Get(id)
	if err != nil || m.Status != patchstore.StatusRejected || !strings.Contains(m.Reason, "more than 1000 bytes") {
		t.Fatalf("meta %+v %v", m, err)
	}
	for _, c := range r.q.Pending(pane) {
		if c.Kind == queue.KindPatch {
			t.Fatalf("a card was made: %+v", c)
		}
	}
}
