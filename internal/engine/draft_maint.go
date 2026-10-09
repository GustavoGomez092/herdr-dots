package engine

import (
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
// on: it was stored but its card was never made (a crash in between), or the
// card is gone or resolved. A card of a closed pane (orphaned) stays: the
// developer can still discard it.
func (e *Engine) orphanedPatch(m patchstore.Meta) bool {
	if m.Status != patchstore.StatusReady {
		return false
	}
	if m.Card == "" {
		return true
	}
	c, ok := e.d.Queue.Get(m.Card)
	if !ok {
		return true
	}
	switch c.Status {
	case queue.StatusPending, queue.StatusAuto, queue.StatusOrphaned:
		return false
	}
	return true
}

// maintainDrafts removes what interrupted drafts left (a "drafting" record
// from before this engine started: its worktree, its snapshot ref and the
// record), expires sent, applied and rejected patches after patchKeep,
// removes ready patches that lost their card, and prunes orphaned worktree
// directories and git's worktree metadata (unlocking stale registrations).
// Ready patches with a card stay until the user applies, sends or discards
// them.
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
	e.mu.Unlock()
	ctx, g := e.ctx, e.d.Git
	repos := map[string]bool{}
	for _, m := range metas {
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
		if !interrupted && !expired && !orphan {
			continue
		}
		if interrupted {
			if err := os.RemoveAll(filepath.Join(e.d.WorktreesDir, m.ID)); err != nil {
				e.d.Logf("draft %s: %v", m.ID, err)
			}
		}
		if m.Repo != "" {
			if err := worktree.DropSnapshot(ctx, g, m.Repo, m.ID); err != nil {
				e.d.Logf("draft %s: drop snapshot ref: %v", m.ID, err)
			}
		}
		if err := e.d.Patches.Delete(m.ID); err != nil {
			e.d.Logf("draft %s: %v", m.ID, err)
		}
		switch {
		case interrupted:
			e.d.Logf("draft %s by %s was interrupted (engine stopped); cleaned up", m.ID, m.Dot)
		case orphan:
			e.d.Logf("patch %s by %s had no card; removed", m.ID, m.Dot)
		}
	}
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
