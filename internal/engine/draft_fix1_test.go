package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
)

func (r *draftRig) noDraftsInFlight(t *testing.T) {
	t.Helper()
	eventually(t, "the draft goroutines ended", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return len(r.e.drafts) == 0
	})
}

// Fix 1: a draft belongs to the attachment it was started for. Closing the
// pane abandons it (even when the backend ignores cancellation); a reused pane
// id gets none of its card or cost, and the dot is free to draft again.
func TestAClosedPaneAbandonsItsDraftEvenWhenThePaneIdIsReused(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release) // a failed assertion must not leave a deaf backend blocked
	r.dr.mu.Lock()
	r.dr.gate, r.dr.edit, r.dr.deaf = gate, fixJWT, true
	r.dr.mu.Unlock()
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	id1, err := r.e.Draft(context.Background(), pane, "security", "fix", "")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the first draft runs", func() bool { return len(r.dr.writes()) == 1 })
	r.e.dropPane(pane)
	if p, _ := r.e.Snapshot().Pane(pane); len(p.Drafts) != 0 {
		t.Fatal("an abandoned draft must not show as running")
	}
	r.attach(t, "security") // the same pane id, a new attachment
	r.reviewsDone(t, 1)
	calls := r.usage.Calls(pane, "security")
	id2, err := r.e.Draft(context.Background(), pane, "security", "fix again", "")
	if err != nil {
		t.Fatalf("the dot must be free to draft again at once: %v", err)
	}
	release()
	r.noDraftsInFlight(t)
	var cards []queue.Card
	for _, c := range r.q.All() {
		if c.Kind == queue.KindPatch && c.Status == queue.StatusPending {
			cards = append(cards, c)
		}
	}
	if len(cards) != 1 || cards[0].PatchID != id2 {
		t.Fatalf("want one card, for the second draft %s: %+v", id2, cards)
	}
	if got := r.usage.Calls(pane, "security"); got != calls+1 {
		t.Fatalf("calls %d, want %d: only the second draft belongs to this attachment", got, calls+1)
	}
	if _, err := r.patches.Get(id1); !errors.Is(err, patchstore.ErrNotFound) {
		t.Fatalf("the abandoned draft left a record: %v", err)
	}
	if ents, _ := os.ReadDir(r.wts); len(ents) != 0 {
		t.Fatalf("worktrees left: %v", ents)
	}
	if refs := r.refs(t); strings.Contains(refs, id1) {
		t.Fatalf("the abandoned draft's ref is left: %s", refs)
	}
}

// Fix 2: the record is written first and deleted last, so a kill during
// cleanup leaves the record the next start's sweep needs to find the ref
// (which holds the user's uncommitted files) and the worktree.
func TestAKillDuringCleanupLeavesTheRecordForTheNextSweep(t *testing.T) {
	for _, stop := range []string{"worktree", "ref", "record"} {
		t.Run(stop, func(t *testing.T) {
			r := newDraftRig(t, draftOpts{}, writeDot("Security"))
			r.dr.set(nil, errors.New("boom"))
			r.attach(t, "security")
			r.e.cleanupStop = func(step string) bool { return step == stop }
			id, err := r.e.Draft(context.Background(), pane, "security", "fix", "")
			if err != nil {
				t.Fatal(err)
			}
			r.noDraftsInFlight(t)
			if _, err := r.patches.Get(id); err != nil {
				t.Fatalf("the record must outlive every other cleanup step: %v", err)
			}
			if stop == "worktree" {
				if _, err := os.Stat(r.wts + "/" + id); err != nil {
					t.Fatalf("worktree: %v", err)
				}
			}
			if stop != "record" && !strings.Contains(r.refs(t), id) {
				t.Fatal("the ref must still be there")
			}
			// The next engine start.
			r.e.mu.Lock()
			r.e.startedAt = time.Now().Add(time.Minute)
			r.e.mu.Unlock()
			r.e.cleanupStop = nil
			r.e.maintainDrafts()
			if _, err := r.patches.Get(id); !errors.Is(err, patchstore.ErrNotFound) {
				t.Fatalf("record left: %v", err)
			}
			if ents, _ := os.ReadDir(r.wts); len(ents) != 0 {
				t.Fatalf("worktree left: %v", ents)
			}
			if r.refs(t) != "" || strings.Count(gitT(t, r.repo, "worktree", "list", "--porcelain"), "worktree ") != 1 {
				t.Fatal("ref or worktree registration left")
			}
		})
	}
}

// Fix 3: apply and discard share one per-card reservation, in both orders.
func TestDiscardAndApplyShareOneReservation(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	r.noDraftsInFlight(t)

	// Apply in progress: a discard is refused and changes nothing.
	r.e.mu.Lock()
	r.e.applying[c.ID] = true
	r.e.mu.Unlock()
	if err := r.e.DiscardPatch(c.ID); err == nil {
		t.Fatal("a discard during an apply must be refused")
	}
	if err := r.e.Dismiss(c.ID); err == nil {
		t.Fatal("a dismiss during an apply must be refused")
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusPending {
		t.Fatalf("card %+v", got)
	}
	if _, err := r.patches.Get(c.PatchID); err != nil {
		t.Fatal("the patch must survive a refused discard")
	}
	r.e.mu.Lock()
	delete(r.e.applying, c.ID)
	r.e.mu.Unlock()

	// Discard first: an apply afterwards is refused and applies nothing.
	if err := r.e.DiscardPatch(c.ID); err != nil {
		t.Fatal(err)
	}
	r.e.mu.Lock()
	if len(r.e.applying) != 0 {
		r.e.mu.Unlock()
		t.Fatal("a discard must release its reservation")
	}
	r.e.mu.Unlock()
	if err := r.applyReviewed(c.ID); err == nil {
		t.Fatal("applying a discarded patch must fail")
	}
	if got, _ := os.ReadFile(r.repo + "/auth.js"); strings.Contains(string(got), "setCookie") {
		t.Fatal("a discarded patch was applied")
	}
}

// Fix 4: dismissing a patch card sets its status under dmu, like any card.
func TestDismissingAPatchCardWaitsForDeliveries(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	r.noDraftsInFlight(t)
	r.e.dmu.Lock()
	done := make(chan error, 1)
	go func() { done <- r.e.Dismiss(c.ID) }()
	select {
	case err := <-done:
		r.e.dmu.Unlock()
		t.Fatalf("Dismiss ran while a delivery held dmu: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusPending {
		r.e.dmu.Unlock()
		t.Fatalf("status changed under dmu: %+v", got)
	}
	r.e.dmu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusDismissed {
		t.Fatalf("card %+v", got)
	}
}

// Fix 6: memory gets the unredacted summary, so a secret in it leaves a
// redacted ledger line and no entry (memory.ScanSecrets must still see it).
func TestASecretInAPatchSummaryNeverBecomesAMemoryEntry(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	key := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789ABCDEF"
	r.dr.mu.Lock()
	r.dr.msg = "Rotated " + key + " out of the config."
	r.dr.mu.Unlock()
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	if strings.Contains(c.Body, key) {
		t.Fatalf("the card shows the key: %q", c.Body)
	}
	if err := r.applyReviewed(c.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a ledger line", func() bool {
		l, _ := r.mem.Ledger(testProject)
		return len(l) == 1
	})
	l, _ := r.mem.Ledger(testProject)
	if l[0].Kind != memory.OutcomeApplied || strings.Contains(l[0].Body, "sk-ant") || l[0].Body == "" {
		t.Fatalf("ledger %+v: want a redacted line", l[0])
	}
	if es, _ := r.mem.List(testProject, memory.Filter{Scope: memory.ScopeDot, Dot: "security"}); len(es) != 0 {
		t.Fatalf("a secret-looking summary made an entry: %+v", es)
	}
}
