package engine

import (
	"context"
	"fmt"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

// Summon runs one dot now on recent turns. It ignores mode, filter and budget
// (usage is still counted) and always produces a card.
func (e *Engine) Summon(ctx context.Context, paneID, slug, question string) error {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrNoAttachment, paneID)
	}
	if att.Find(slug) == nil {
		return fmt.Errorf("dot %q is not attached to %s", slug, paneID)
	}
	e.mu.Lock()
	d, ok := e.dots[slug]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown dot %q", slug)
	}
	turns, err := e.reader(att).Turns(ctx)
	if err != nil {
		return fmt.Errorf("read conversation: %w", err)
	}
	turns = tail(turns, d.Context.Turns)
	e.spawn(func() { e.runDot(paneID, d, turns, runOpts{summon: true, question: question}) })
	return nil
}

func (e *Engine) SetMode(slug string, mode dots.Mode) error {
	if !mode.Valid() {
		return fmt.Errorf("invalid mode %q", mode)
	}
	e.mu.Lock()
	d, ok := e.dots[slug]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown dot %q", slug)
	}
	d.Delivery.Mode = mode
	if err := e.d.SaveDot(d); err != nil {
		return err
	}
	e.ReloadDots()
	return nil
}

func (e *Engine) SetPaused(paneID, slug string, paused bool) error {
	found := false
	err := e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		if da := a.Find(slug); da != nil {
			found = true
			da.Paused = paused
			if !paused {
				da.Failures = 0
			}
		}
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("dot %q is not attached to %s", slug, paneID)
	}
	if !paused {
		e.setStatus(key{paneID, slug}, view.StatusIdle)
		return nil
	}
	e.mu.Lock()
	delete(e.status, key{paneID, slug})
	e.mu.Unlock()
	e.broadcast()
	return nil
}

func (e *Engine) TogglePauseAll() {
	e.mu.Lock()
	e.pausedAll = !e.pausedAll
	e.mu.Unlock()
	e.broadcast()
}

// Reset clears a dot's failure stop and budget warning.
func (e *Engine) Reset(paneID, slug string) error {
	if err := e.SetPaused(paneID, slug, false); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.warned, key{paneID, slug})
	e.mu.Unlock()
	return nil
}
