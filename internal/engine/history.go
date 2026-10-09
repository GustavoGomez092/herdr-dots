package engine

import (
	"slices"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// historyKind is the run kind of a runDot run. Runs that don't go through
// runDot (Phase 7 drafts, Phase 8 background runs) pass their kind to trace.
func (o runOpts) historyKind() string {
	switch {
	case o.memoryPass:
		return history.KindMemory
	case o.ask: // Phase 5 MCP ask_dot
		return history.KindAsk
	case o.noForce: // Phase 3 council member
		return history.KindCouncil
	case o.summon:
		return history.KindSummon
	}
	return history.KindReview
}

// outcomeDropped is the history outcome of a run that reached its backend but
// ended with no feed line of its own: its pane closed, its dot was detached,
// its pane id was reused (the generation changed), its ask or council was
// abandoned, or the engine stopped. It still records what the run was
// charged, so profile cost stats see every dollar the daily total does.
const outcomeDropped = "dropped"

// runTrace collects one run's history record. The run marks it next to each
// of its feed lines and appends it once when it ends; a run that never
// reached its backend (cancelled while waiting for a slot) leaves no record,
// and one that did but ended without a mark is recorded as outcomeDropped.
type runTrace struct {
	e      *Engine
	rec    history.Record
	marked bool
}

func (e *Engine) trace(paneID string, d dots.Dot, kind, project string) *runTrace {
	return &runTrace{e: e, rec: history.Record{T: history.TypeRun, Dot: d.Slug, Pane: paneID, Kind: kind, Project: project}}
}

// mark records how the run ended; the last mark wins.
func (t *runTrace) mark(turn int, outcome, detail string) {
	if t == nil {
		return
	}
	t.rec.Turn, t.rec.Outcome, t.rec.Excerpt, t.marked = turn, outcome, detail, true
}

// markVerdict is mark for a verdict: the excerpt is the dot's message.
func (t *runTrace) markVerdict(turn int, outcome string, v backend.Verdict) {
	if t == nil {
		return
	}
	t.mark(turn, outcome, v.Message)
	t.rec.Severity = v.Severity
}

// usage records the backend's tokens, cost and latency. Cost provenance
// follows Phase 4's deltaOf: a run that reported no usage at all cost nothing
// known; one with tokens but no price is unpriced. From here on the run is
// recorded even if no mark follows (outcomeDropped): the call happened.
func (t *runTrace) usage(res backend.Result, took time.Duration) {
	if t == nil {
		return
	}
	d := deltaOf(res)
	t.rec.In, t.rec.Out, t.rec.Ms = res.InputTokens, res.OutputTokens, took.Milliseconds()
	t.rec.CostUSD, t.rec.CostUnknown = 0, !d.CostKnown
	if d.CostKnown {
		t.rec.CostUSD = res.CostUSD
	}
	if !t.marked {
		t.rec.Outcome, t.marked = outcomeDropped, true
	}
}

// verdictOutcome is the history outcome of a verdict that ends a run with no
// feed outcome of its own (an MCP ask's feed line says "asked").
func verdictOutcome(v backend.Verdict) string {
	if v.Relevant {
		return state.OutcomeFlagged
	}
	return state.OutcomePass
}

// card links the run to the card it produced.
func (t *runTrace) card(c queue.Card) {
	if t == nil {
		return
	}
	t.rec.Card, t.rec.Severity = history.CardKey(t.e.d.SessionKey, c.ID), c.Severity
}

// done appends the record. It is called on the run's own goroutine, which
// holds no engine lock (history I/O never happens under paneLock, mu or dmu),
// but may still hold a run slot (sem, bgSem): the append is spawned, so a
// history file locked by another session never keeps the next run waiting.
// After Wait (spawn refuses) the run's goroutine, itself tracked, writes it.
func (t *runTrace) done() {
	if t == nil || !t.marked || t.e.d.History == nil {
		return
	}
	e, rec := t.e, t.rec
	write := func() {
		if err := e.d.History.Append(rec); err != nil {
			e.d.Logf("history %s: %v", rec.Dot, err)
		}
	}
	if !e.spawn(write) {
		write()
	}
}

// resolutionFor maps a memory outcome kind to a history resolution.
func resolutionFor(kind string, edited bool) string {
	switch kind {
	case memory.OutcomeSent:
		if edited {
			return history.ResEdited
		}
		return history.ResSent
	case memory.OutcomeEdited:
		return history.ResEdited
	case memory.OutcomeDismissed:
		return history.ResDismissed
	case "applied": // Phase 7 patch outcomes (memory.Outcome kinds)
		return history.ResApplied
	case "discarded":
		return history.ResDiscarded
	}
	return ""
}

// spawnResolve records what happened to a card, off the caller's locks
// (Dismiss and deliver hold dmu). A council card (Phase 3) or an away digest
// (Phase 8, Dot "away") speaks for several dots: its resolution is recorded
// for each dot that contributed a part, as memory records its outcomes.
func (e *Engine) spawnResolve(c queue.Card, resolution string) {
	if e.d.History == nil || resolution == "" {
		return
	}
	slugs := []string{c.Dot}
	if c.Kind == queue.KindCouncil || c.Kind == queue.KindDigest {
		slugs = nil
		for _, p := range c.Parts {
			if !p.Failed && p.Severity != "" && !slices.Contains(slugs, p.Dot) {
				slugs = append(slugs, p.Dot)
			}
		}
	}
	var recs []history.Record
	for _, slug := range slugs {
		if slug == "" || slug == dots.ReservedSlug || !history.ValidSlug(slug) {
			continue
		}
		recs = append(recs, history.Record{T: history.TypeResolve, Dot: slug, Pane: c.PaneID, Project: c.Project,
			Card: history.CardKey(e.d.SessionKey, c.ID), Resolution: resolution})
	}
	if len(recs) == 0 {
		return
	}
	e.spawn(func() {
		for _, r := range recs {
			if err := e.d.History.Append(r); err != nil {
				e.d.Logf("history %s: %v", r.Dot, err)
			}
		}
	})
}
