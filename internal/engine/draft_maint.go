package engine

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/worktree"
)

const (
	draftMaintInterval = 24 * time.Hour     // spec §6 step 12: at start and daily
	patchKeep          = 7 * 24 * time.Hour // sent, applied and rejected patches
	staleWorktree      = time.Hour          // a worktree dir this old without a live draft is orphaned
)

// draftLoop cleans up after drafts at start and then daily.
func (e *Engine) draftLoop() {
	e.maintainDrafts()
	t := time.NewTicker(draftMaintInterval)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			e.maintainDrafts()
		}
	}
}

// orphanedPatch reports whether a ready patch has no card the user could act
// on: its card was never made (a crash between storing the patch and adding
// the card), or the card is gone or resolved. The card is found by the
// patch id it carries, not by the record's card field (a crash between
// Queue.Add and that write leaves the field empty). A card of a closed pane
// (orphaned) stays: the developer can still discard it.
func (e *Engine) orphanedPatch(m patchstore.Meta) bool {
	if m.Status != patchstore.StatusReady {
		return false
	}
	for _, c := range e.d.Queue.All() {
		if c.Kind != queue.KindPatch || c.PatchID != m.ID {
			continue
		}
		switch c.Status {
		case queue.StatusPending, queue.StatusAuto, queue.StatusOrphaned:
			return false
		}
	}
	return true
}

// maintainDrafts removes what interrupted drafts left (a "drafting" record
// from before this engine started: its worktree, its snapshot ref and the
// record), expires sent, applied and rejected patches after patchKeep,
// removes ready patches that lost their card, retries snapshot refs whose
// drop failed, drops snapshot refs nothing owns (spec X8), and prunes
// orphaned worktree directories and git's worktree metadata (unlocking stale
// registrations). Ready patches with a card stay until the user applies,
// sends or discards them. A record is only deleted once its ref is gone: a
// failed drop leaves it drop-pending for the next sweep.
func (e *Engine) maintainDrafts() {
	metas, errs := e.d.Patches.List()
	for _, err := range errs {
		e.d.Logf("drafts: %v", err)
	}
	e.mu.Lock()
	live := map[string]bool{}
	for id := range e.drafts {
		live[id] = true
	}
	started := e.startedAt
	cwds := map[string]bool{}
	for _, a := range e.d.Attachments.List() { // e.mu -> stores
		if a.Cwd != "" {
			cwds[a.Cwd] = true
		}
	}
	e.mu.Unlock()
	ctx, g := e.ctx, e.d.Git
	repos := map[string]bool{}
	status := map[string]string{} // every readable record, by id
	for _, m := range metas {
		status[m.ID] = m.Status
		if m.Repo != "" {
			repos[m.Repo] = true
		}
		if live[m.ID] {
			continue
		}
		interrupted := m.Status == patchstore.StatusDrafting && m.UpdatedAt.Before(started)
		expired := (m.Status == patchstore.StatusSent || m.Status == patchstore.StatusApplied || m.Status == patchstore.StatusRejected) &&
			time.Since(m.UpdatedAt) > patchKeep
		orphan := m.UpdatedAt.Before(started) && e.orphanedPatch(m)
		pending := m.Status == patchstore.StatusDropPending
		if !interrupted && !expired && !orphan && !pending {
			continue
		}
		if interrupted {
			if err := os.RemoveAll(filepath.Join(e.d.WorktreesDir, m.ID)); err != nil {
				e.d.Logf("draft %s: %v", m.ID, err)
			}
		}
		if m.Repo != "" {
			if err := worktree.DropSnapshot(ctx, g, m.Repo, m.ID); err != nil {
				e.d.Logf("draft %s: drop snapshot ref (kept for the next sweep): %v", m.ID, err)
				if !pending {
					if _, err := e.d.Patches.Update(m.ID, func(m *patchstore.Meta) { m.Status = patchstore.StatusDropPending }); err != nil {
						e.d.Logf("draft %s: %v", m.ID, err)
					}
				}
				continue
			}
		}
		if err := e.d.Patches.Delete(m.ID); err != nil {
			e.d.Logf("draft %s: %v", m.ID, err)
		}
		delete(status, m.ID)
		switch {
		case interrupted:
			e.d.Logf("draft %s by %s was interrupted (engine stopped); cleaned up", m.ID, m.Dot)
		case orphan:
			e.d.Logf("patch %s by %s had no card; removed", m.ID, m.Dot)
		}
	}
	e.sweepRefs(repos, cwds, status)
	if len(repos) == 0 {
		repos[""] = true // still delete stale worktree directories
	}
	for repo := range repos {
		if n, err := worktree.Prune(ctx, g, repo, e.d.WorktreesDir, staleWorktree, live); err != nil {
			e.d.Logf("drafts: prune %s: %v", repo, err)
		} else if n > 0 {
			e.d.Logf("drafts: removed %d orphaned worktree(s)", n)
		}
	}
}

// sweepRefs lists the snapshot refs in every repository a record names and
// every repository a pane is attached in, and drops those no ready or sent
// patch needs: an applied or rejected record's (a drop that failed before),
// and any with no record and no live draft. Ownership is checked again just
// before such a drop (a draft's record is written before its ref and deleted
// after it), and an unreadable record keeps its ref. No lock is held over git.
func (e *Engine) sweepRefs(repos, cwds map[string]bool, status map[string]string) {
	ctx, g := e.ctx, e.d.Git
	scan := map[string]bool{}
	for r := range repos {
		scan[r] = true
	}
	for cwd := range cwds {
		if top, err := worktree.RepoRoot(ctx, g, cwd); err == nil {
			scan[top] = true
		}
	}
	for repo := range scan {
		ids, err := worktree.SnapshotIDs(ctx, g, repo)
		if err != nil {
			e.d.Logf("drafts: list snapshot refs in %s: %v", repo, err)
			continue
		}
		for _, id := range ids {
			st, recorded := status[id]
			switch {
			case recorded && st != patchstore.StatusApplied && st != patchstore.StatusRejected:
				continue // ready, sent, drafting or drop-pending: not this sweep's to drop
			case !recorded && e.refOwned(id):
				continue
			}
			if err := worktree.DropSnapshot(ctx, g, repo, id); err != nil {
				e.d.Logf("drafts: drop snapshot ref %s: %v", id, err)
			} else if !recorded {
				e.d.Logf("drafts: dropped snapshot ref %s that no patch owned", id)
			}
		}
	}
}

// refOwned reports whether a snapshot ref may still be needed: a draft with
// that id is running, or it has a record (or one that can't be read).
func (e *Engine) refOwned(id string) bool {
	e.mu.Lock()
	_, running := e.drafts[id]
	e.mu.Unlock()
	if running {
		return true
	}
	_, err := e.d.Patches.Get(id)
	return !errors.Is(err, patchstore.ErrNotFound)
}
