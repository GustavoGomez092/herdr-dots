package engine

import (
	"errors"
	"fmt"
	"time"

	"herdr-dots/internal/delivery"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
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
	gate, err := delivery.Send(e.ctx, e.d.Herdr, paneID, cards, inject)
	for _, c := range cards {
		switch {
		case err != nil:
			_ = e.d.Queue.Fail(c.ID, err.Error())
		case gate == delivery.Now:
			_ = e.d.Queue.SetStatus(c.ID, queue.StatusSent)
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

// SendCards approves cards and delivers them now, or as soon as the agent is idle.
func (e *Engine) SendCards(ids []string) error {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	byPane := map[string][]queue.Card{}
	var order []string
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
	e.dmu.Lock()
	defer e.dmu.Unlock()
	c, ok := e.d.Queue.Get(id)
	if !ok {
		return queue.ErrNotFound
	}
	if err := e.d.Queue.SetStatus(id, queue.StatusDismissed); err != nil {
		return err
	}
	e.reportPending(c.PaneID)
	e.broadcast()
	return nil
}

func (e *Engine) Edit(id, body string) error {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	if err := e.d.Queue.Edit(id, body); err != nil {
		return err
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
		e.flushAuto(a.PaneID)
	}
}

func (e *Engine) dropPane(paneID string) {
	e.unwatch(paneID)
	// Orphan cards first: the attachment disappearing is the visible signal that
	// the pane is fully handled.
	if n, err := e.d.Queue.OrphanPane(paneID); err != nil {
		e.d.Logf("orphan cards for %s: %v", paneID, err)
	} else if n > 0 {
		e.d.Logf("pane %s closed: %d card(s) orphaned", paneID, n)
	}
	_ = e.d.Usage.ResetPane(paneID)
	_ = e.d.Attachments.Delete(paneID)
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
