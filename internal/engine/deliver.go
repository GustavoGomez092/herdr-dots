package engine

import (
	"errors"
	"fmt"
	"time"

	"herdr-dots/internal/delivery"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

// flushAuto delivers waiting cards: approved ones need an idle agent; inject
// ones also need the developer to be out of that pane.
func (e *Engine) flushAuto(paneID string) {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	var approved, inject []queue.Card
	for _, c := range e.d.Queue.Pending(paneID) {
		if c.Status != queue.StatusAuto {
			continue
		}
		if c.Approved {
			approved = append(approved, c)
		} else {
			inject = append(inject, c)
		}
	}
	if len(approved) > 0 {
		e.deliver(paneID, approved, false)
	}
	if len(inject) > 0 {
		e.deliver(paneID, inject, true)
	}
}

// deliver must be called with e.dmu held.
func (e *Engine) deliver(paneID string, cards []queue.Card, inject bool) {
	gate, err := delivery.Send(e.ctx, e.d.Herdr, paneID, e.forDelivery(cards), inject)
	for _, c := range cards {
		switch {
		case err != nil:
			_ = e.d.Queue.Fail(c.ID, err.Error())
		case gate == delivery.Now:
			// Only cards the developer approved are accepted findings; inject
			// deliveries were never approved.
			if err := e.d.Queue.SetStatus(c.ID, queue.StatusSent); err == nil && c.Approved {
				if c.Kind == queue.KindPatch {
					e.patchSent(c) // marks the patch sent and records the outcome
				} else {
					e.spawnOutcome(c, memory.OutcomeSent)
				}
			}
		case c.Status == queue.StatusPending:
			_ = e.d.Queue.SetStatus(c.ID, queue.StatusAuto)
		}
	}
	if err != nil {
		e.d.Logf("deliver to %s: %v", paneID, err)
	}
	e.reportPending(paneID)
	e.broadcast()
}

// SendCards sends cards with no digest part counts, so it refuses any digest.
func (e *Engine) SendCards(ids []string) error { return e.SendCardsSeen(ids, nil) }

// SendCardsSeen approves cards and delivers them now, or as soon as the agent
// is idle. seen maps each digest to the part count the developer was shown; a
// digest whose current count differs, or that has no entry, refuses the whole
// batch (a digest can gain parts after it was read).
func (e *Engine) SendCardsSeen(ids []string, seen map[string]int) error {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	for _, id := range ids {
		if c, ok := e.d.Queue.Get(id); ok && c.Kind == queue.KindDigest {
			if n, has := seen[id]; !has || n != len(c.Parts) {
				return errors.New("the digest changed since you read it: expand it again")
			}
		}
	}
	byPane := map[string][]queue.Card{}
	var order []string
	for _, id := range ids {
		if c, ok := e.d.Queue.Get(id); ok && c.Kind == queue.KindPatch && e.cardReserved(id) {
			return fmt.Errorf("card %s is being applied or discarded", id)
		}
	}
	for _, id := range ids {
		c, ok := e.d.Queue.Get(id)
		if !ok {
			return fmt.Errorf("card %s: %w", id, queue.ErrNotFound)
		}
		if c.Mode == string(dots.ModeSuggest) {
			return fmt.Errorf("card %s is a suggestion; copy it instead of sending", id)
		}
		if err := e.d.Queue.Approve(id); err != nil {
			return err
		}
		c.Approved = true
		if _, ok := byPane[c.PaneID]; !ok {
			order = append(order, c.PaneID)
		}
		byPane[c.PaneID] = append(byPane[c.PaneID], c)
	}
	for _, p := range order {
		e.deliver(p, byPane[p], false)
	}
	return nil
}

// Dismiss and Edit hold e.dmu so they never interleave with a delivery: a card
// is either sent as it was, or dismissed/edited before any flush reads it.
func (e *Engine) Dismiss(id string) error {
	if c, ok := e.d.Queue.Get(id); ok && c.Kind == queue.KindPatch {
		return e.DiscardPatch(id) // x on a patch card discards the patch too
	}
	e.dmu.Lock()
	defer e.dmu.Unlock()
	c, ok := e.d.Queue.Get(id)
	if !ok {
		return queue.ErrNotFound
	}
	if err := e.d.Queue.SetStatus(id, queue.StatusDismissed); err != nil {
		return err
	}
	e.spawnOutcome(c, memory.OutcomeDismissed)
	e.reportPending(c.PaneID)
	e.broadcast()
	return nil
}

func (e *Engine) Edit(id, body string) error {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	before, ok := e.d.Queue.Get(id)
	if ok && before.Kind == queue.KindPatch {
		return errors.New("a patch card can't be edited: discard it and draft again")
	}
	if err := e.d.Queue.Edit(id, body); err != nil {
		return err
	}
	if ok {
		e.mu.Lock()
		if _, seen := e.origBody[id]; !seen {
			e.origBody[id] = before.Body // the ledger keeps what the dot said
		}
		e.mu.Unlock()
	}
	e.broadcast()
	return nil
}

func (e *Engine) pollLoop() {
	t := time.NewTicker(e.d.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			e.poll()
		}
	}
}

// poll detects closed panes and session changes (Herdr has no pane-closed
// event in its subscription stream) and flushes auto cards whose guard opened.
func (e *Engine) poll() {
	e.maybeReloadConfig()
	for _, a := range e.d.Attachments.List() {
		p, err := e.d.Herdr.PaneGet(e.ctx, a.PaneID)
		if herdr.IsNotFound(err) {
			e.dropPane(a.PaneID)
			continue
		}
		if err != nil {
			continue
		}
		e.checkSession(a, p)
		e.observePane(a, p) // idle periods, the away digest, research (idle.go)
		e.flushAuto(a.PaneID)
	}
	e.todayTick()
	e.inboxTick()
	e.scheduleTick()
}

func (e *Engine) dropPane(paneID string) {
	// First: a draft running for this pane ends without a card or a charge,
	// even if the pane id is reused by a new attachment.
	e.abandonDrafts(paneID, "")
	e.abandonCouncil(paneID) // a closed pane gets no council card
	e.unwatch(paneID)
	// Under e.mu (e.mu -> stores), as charge and restoreStatus check the
	// attachment: a run ending now either lands before the reset or sees the
	// pane gone, and a reused pane id inherits no spend or status. The cards
	// are orphaned in the same section, after the generation bump and before
	// the delete: a card added under e.mu with a pane check (a scheduled
	// result, a claimed inbox card) either lands before it and is orphaned
	// here, or sees the pane gone. Snapshots take e.mu too, so none shows the
	// attachment gone with its cards still pending.
	e.hook("drop-before-lock")
	e.mu.Lock()
	e.abandonDraftsLocked(paneID, "") // same section as the delete (see Draft)
	e.gens[paneID]++
	delete(e.idle, paneID) // collected research for a closed pane is dropped (idle.go)
	orphaned, oerr := e.d.Queue.OrphanPane(paneID)
	_ = e.d.Usage.ResetPane(paneID)
	_ = e.d.Attachments.Delete(paneID)
	for k := range e.status {
		if k.pane == paneID {
			delete(e.status, k)
		}
	}
	e.mu.Unlock()
	if oerr != nil {
		e.d.Logf("orphan cards for %s: %v", paneID, oerr)
	} else if orphaned > 0 {
		e.d.Logf("pane %s closed: %d card(s) orphaned", paneID, orphaned)
	}
	// After the attachment is gone: in-flight MCP asks end with ErrPaneClosed
	// and nothing can re-mark the dead pane.
	e.cancelAsks(paneID)
	e.forgetAsks(paneID)
	e.broadcast()
}

// checkSession follows a new agent session in the same pane (/clear, restart).
// Holds the pane lock for its whole body, and re-checks the session inside the
// update so concurrent callers with a stale snapshot reset LastSeenTurn once.
func (e *Engine) checkSession(a state.Attachment, p herdr.PaneInfo) {
	if p.AgentSession == nil {
		return
	}
	pl := e.paneLock(a.PaneID)
	pl.Lock()
	defer pl.Unlock()
	changed := false
	var slugs []string
	err := e.d.Attachments.Update(a.PaneID, func(x *state.Attachment) {
		if x.SessionValue == p.AgentSession.Value {
			return
		}
		changed = true
		e.d.Logf("pane %s: agent session %q → %q", a.PaneID, x.SessionValue, p.AgentSession.Value)
		x.SessionKind, x.SessionValue = p.AgentSession.Kind, p.AgentSession.Value
		if p.Agent != "" {
			x.Agent = p.Agent
		}
		x.ForceScrollback = false
		for i := range x.Dots {
			x.Dots[i].LastSeenTurn = 0
			x.Dots[i].MemorySeenTurn = 0 // turn indexes restart with the session
			x.Dots[i].MemoryPasses = 0   // a per-session budget, like the call budgets
			slugs = append(slugs, x.Dots[i].Slug)
		}
	})
	if errors.Is(err, state.ErrNoAttachment) || !changed {
		return
	}
	// Budgets are per agent session: a new session starts from zero.
	if err := e.d.Usage.ResetPane(a.PaneID); err != nil {
		e.d.Logf("reset usage %s: %v", a.PaneID, err)
	}
	e.mu.Lock()
	for _, s := range slugs {
		k := key{a.PaneID, s}
		delete(e.warned, k)
		if e.status[k] == view.StatusBudget {
			delete(e.status, k)
		}
	}
	e.mu.Unlock()
	e.dropReader(a.PaneID)
	if att, ok := e.d.Attachments.Get(a.PaneID); ok {
		r := e.reader(att)
		_ = e.d.Attachments.Update(a.PaneID, func(x *state.Attachment) { x.Source = r.Source() })
	}
	if e.memEnabled() {
		cwd := p.WorkDir()
		e.spawn(func() { e.reresolve(a.PaneID, cwd) }) // runs git: never under the pane lock
	}
}

func (e *Engine) reportPending(paneID string) {
	a, ok := e.d.Attachments.Get(paneID)
	agent := a.Agent
	if !ok || agent == "" {
		agent = "agent"
	}
	if err := e.d.Herdr.ReportPending(e.ctx, paneID, agent, len(e.d.Queue.Pending(paneID))); err != nil && !herdr.IsNotFound(err) {
		e.d.Logf("report pending: %v", err)
	}
}
