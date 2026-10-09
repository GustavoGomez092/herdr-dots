package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"herdr-dots/internal/council"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/state"
	"herdr-dots/internal/textutil"
)

// AskCouncil convenes every eligible attached dot for the MCP server's
// ask_council and returns the merged digest instead of adding a council
// card. It counts as one ask against the [mcp] limits; members that answer
// are marked consulted. If ctx ends first, the council is abandoned. It
// always passes nil slugs (every eligible dot) and never relies on telling an
// empty list from an absent one.
func (e *Engine) AskCouncil(ctx context.Context, paneID, question string) (ipc.CouncilResult, error) {
	q := strings.TrimSpace(textutil.SanitizeModelText(question))
	if q == "" {
		return ipc.CouncilResult{}, errors.New("the question is empty")
	}
	if _, ok := e.d.Attachments.Get(paneID); !ok {
		return ipc.CouncilResult{}, errors.New("no dots are attached to this pane (the developer attaches them with prefix+d)")
	}
	lim, mcp := e.limiter(), e.config().MCP.Normalized()
	before := e.d.Usage.PaneCost(paneID)
	type outcome struct {
		parts      []council.Part
		body, note string
	}
	got := make(chan outcome, 1)
	run, err := e.startCouncil(ctx, paneID, q, nil, func(parts []council.Part, body, note string) {
		got <- outcome{parts, body, note}
	}, func() error { return lim.take(paneID, mcp) }) // the slot is used only once the council starts
	if err != nil {
		return ipc.CouncilResult{}, err
	}
	select {
	case <-run.finished:
	case <-ctx.Done():
		e.stopCouncil(run)
		return ipc.CouncilResult{}, fmt.Errorf("council: %w", ctx.Err())
	case <-e.ctx.Done():
		return ipc.CouncilResult{}, errors.New("council: the dots engine stopped")
	}
	var o outcome
	select {
	case o = <-got:
	default:
		// No digest: Phase 3's lifecycle lost the council (engine stopping,
		// pane closed) or no member answered.
		switch {
		case e.ctx.Err() != nil:
			return ipc.CouncilResult{}, errors.New("council: the dots engine stopped")
		case e.councilAbandoned(run):
			return ipc.CouncilResult{}, fmt.Errorf("council ended without an answer: %w", ErrPaneClosed)
		}
		return ipc.CouncilResult{}, errors.New("council failed: no member answered")
	}
	res := ipc.CouncilResult{Digest: mcpText(o.body), Note: mcpText(o.note), CostUSD: max(0, e.d.Usage.PaneCost(paneID)-before), Members: []ipc.CouncilMember{}}
	var heard []string
	for _, p := range o.parts {
		m := ipc.CouncilMember{Slug: p.Slug, Stance: mcpText(p.Stance), Failed: p.Failed()}
		if p.Relevant() {
			m.Severity = p.Verdict.Severity
		}
		res.Members = append(res.Members, m)
		if !p.Failed() {
			heard = append(heard, p.Slug)
		}
	}
	if !e.markConsulted(paneID, heard...) {
		return ipc.CouncilResult{}, fmt.Errorf("council ended without an answer: %w", ErrPaneClosed)
	}
	e.feed(paneID, councilSlug, run.turn, state.OutcomeAsked,
		fmt.Sprintf("%s · %d of %d members answered", council.MaxSeverity(o.parts), council.Answered(o.parts), len(o.parts)))
	return res, nil
}

// stopCouncil abandons a council whose agent gave up: member and chair runs
// are cancelled and no result or card is produced.
func (e *Engine) stopCouncil(run *councilRun) {
	e.mu.Lock()
	e.abandonRunLocked(run)
	e.mu.Unlock()
	run.cancel()
}
