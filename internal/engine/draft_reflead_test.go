package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/view"
	"herdr-dots/internal/worktree"
)

// lockRef makes git fail to delete id's snapshot ref (a stale lock file, as
// a crashed git would leave) until the returned func runs.
func lockRef(t *testing.T, repo, id string) func() {
	t.Helper()
	lock := filepath.Join(repo, ".git", "refs", "herdr-dots", "snapshots", id+".lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
	}
}

func hasRef(t *testing.T, repo, id string) bool {
	t.Helper()
	return strings.Contains(gitT(t, repo, "for-each-ref", "--format=%(refname)", worktree.RefPrefix), worktree.RefPrefix+id)
}

func TestAFailedRefDropKeepsTheRecordForTheNextSweep(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	unlock := lockRef(t, r.repo, c.PatchID)
	if err := r.e.DiscardPatch(c.ID); err != nil {
		t.Fatal(err)
	}
	m, err := r.patches.Get(c.PatchID)
	if err != nil {
		t.Fatalf("the record went although its ref is still there: %v", err)
	}
	if m.Status != patchstore.StatusDropPending || !hasRef(t, r.repo, c.PatchID) {
		t.Fatalf("record %+v, ref kept %v", m, hasRef(t, r.repo, c.PatchID))
	}
	r.e.maintainDrafts() // still locked: nothing is lost
	if _, err := r.patches.Get(c.PatchID); err != nil {
		t.Fatalf("a failed retry deleted the record: %v", err)
	}
	unlock()
	r.e.maintainDrafts()
	if hasRef(t, r.repo, c.PatchID) {
		t.Fatal("the sweep left the ref")
	}
	if _, err := r.patches.Get(c.PatchID); err == nil {
		t.Fatal("the sweep left the record after dropping the ref")
	}
}

func TestAnExpiredRecordWhoseRefWontDropIsKept(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	id := worktree.NewID()
	if _, err := worktree.TakeSnapshot(context.Background(), r.g, r.repo, id, 0); err != nil {
		t.Fatal(err)
	}
	old := patchstore.Open(r.patches.Dir)
	old.Now = func() time.Time { return time.Now().Add(-8 * 24 * time.Hour) }
	if err := old.Put(patchstore.Meta{ID: id, Dot: "security", Pane: pane, Repo: r.repo, Status: patchstore.StatusSent}); err != nil {
		t.Fatal(err)
	}
	unlock := lockRef(t, r.repo, id)
	r.e.maintainDrafts()
	m, err := r.patches.Get(id)
	if err != nil || m.Status != patchstore.StatusDropPending {
		t.Fatalf("expired record with a stuck ref: %+v %v", m, err)
	}
	unlock()
	r.e.maintainDrafts()
	if hasRef(t, r.repo, id) {
		t.Fatal("ref left")
	}
	if _, err := r.patches.Get(id); err == nil {
		t.Fatal("record left")
	}
}

func TestTheSweepDropsStrayRefsButNeverAnotherRecordsRef(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	kept, stray, unreadable := worktree.NewID(), worktree.NewID(), worktree.NewID()
	for _, id := range []string{kept, stray, unreadable} {
		if _, err := worktree.TakeSnapshot(context.Background(), r.g, r.repo, id, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.patches.Put(patchstore.Meta{ID: kept, Dot: "security", Pane: pane, Repo: r.repo, Status: patchstore.StatusSent}); err != nil {
		t.Fatal(err)
	}
	// a record that can't be read might still own its ref: leave it
	if err := os.WriteFile(filepath.Join(r.patches.Dir, unreadable+".json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.e.maintainDrafts()
	if !hasRef(t, r.repo, kept) || !hasRef(t, r.repo, unreadable) {
		t.Fatalf("a record's ref was dropped: %s", r.refs(t))
	}
	if hasRef(t, r.repo, stray) {
		t.Fatalf("stray ref left: %s", r.refs(t))
	}
}

func TestTheSweepDropsStrayRefsInAttachedRepos(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.attach(t, "security") // the pane works in r.repo; no record names it
	stray := worktree.NewID()
	if _, err := worktree.TakeSnapshot(context.Background(), r.g, r.repo, stray, 0); err != nil {
		t.Fatal(err)
	}
	r.e.maintainDrafts()
	if hasRef(t, r.repo, stray) {
		t.Fatalf("stray ref in an attached repo left: %s", r.refs(t))
	}
}

func TestTheSweepNeverDropsALiveDraftsRef(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	gate := make(chan struct{})
	r.dr.mu.Lock()
	r.dr.gate = gate
	r.dr.mu.Unlock()
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	id, err := r.e.Draft(context.Background(), pane, "security", "fix", "")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the draft running", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Drafts) == 1 && p.Drafts[0].Phase == view.DraftRunning
	})
	r.e.maintainDrafts()
	if !hasRef(t, r.repo, id) {
		t.Fatal("the sweep dropped a live draft's ref")
	}
	close(gate)
	c := r.patchCard(t)
	r.draftsDone(t)
	r.e.maintainDrafts()
	if !hasRef(t, r.repo, c.PatchID) {
		t.Fatal("the sweep dropped a ready card's ref")
	}
}
