package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// Errors the Dots MCP server shows the agent (Phase 5).
var (
	ErrAskLimit    = errors.New("ask limit reached; continue without the dot")
	ErrNotAttached = errors.New("no attached dot")
	ErrPaused      = errors.New("paused")
	ErrBudget      = errors.New("budget reached")
	ErrPaneClosed  = ipc.ErrPaneClosed
)

const (
	maxAskQuestion      = 4000 // runes
	purposeSummaryChars = 200
	maxQueueCards       = 20
	maxQueueBody        = 2000
)

// Query implements ipc.Querier: the data-returning commands of the Dots MCP
// server. Each is scoped to the pane in its arguments.
func (e *Engine) Query(ctx context.Context, cmd string, raw json.RawMessage) (any, error) {
	switch cmd {
	case ipc.CmdListDots:
		var a ipc.PaneArgs
		if err := decodePane(raw, &a, &a.PaneID); err != nil {
			return nil, err
		}
		r, err := e.ListDots(a.PaneID)
		return r, err
	case ipc.CmdAsk:
		var a ipc.AskArgs
		if err := decodePane(raw, &a, &a.PaneID); err != nil {
			return nil, err
		}
		r, err := e.Ask(ctx, a.PaneID, a.Slug, a.Question, a.IncludeTurns)
		return r, err
	case ipc.CmdAskCouncil:
		var a ipc.AskCouncilArgs
		if err := decodePane(raw, &a, &a.PaneID); err != nil {
			return nil, err
		}
		r, err := e.AskCouncil(ctx, a.PaneID, a.Question)
		return r, err
	case ipc.CmdListQueue:
		var a ipc.ListQueueArgs
		if err := decodePane(raw, &a, &a.PaneID); err != nil {
			return nil, err
		}
		r, err := e.ListQueue(a.PaneID, a.Status)
		return r, err
	}
	return nil, fmt.Errorf("unknown query %q", cmd)
}

// decodePane decodes query arguments and requires a pane id.
func decodePane(raw json.RawMessage, v any, pane *string) error {
	if err := decode(raw, v); err != nil {
		return err
	}
	if strings.TrimSpace(*pane) == "" {
		return errors.New("missing pane id")
	}
	return nil
}

// askLimiter enforces [mcp] max_asks_per_turn and max_asks_per_minute per
// pane. Its mutex is a leaf lock.
type askLimiter struct {
	mu     sync.Mutex
	now    func() time.Time
	turn   map[string]int
	recent map[string][]time.Time
}

func newAskLimiter(now func() time.Time) *askLimiter {
	return &askLimiter{now: now, turn: map[string]int{}, recent: map[string][]time.Time{}}
}

// take counts one ask for the pane, or refuses with ErrAskLimit.
func (l *askLimiter) take(paneID string, lim config.MCPConfig) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var keep []time.Time
	for _, t := range l.recent[paneID] {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	l.recent[paneID] = keep
	if l.turn[paneID] >= lim.MaxAsksPerTurn {
		return fmt.Errorf("%w (%d per turn)", ErrAskLimit, lim.MaxAsksPerTurn)
	}
	if len(keep) >= lim.MaxAsksPerMinute {
		return fmt.Errorf("%w (%d per minute)", ErrAskLimit, lim.MaxAsksPerMinute)
	}
	l.turn[paneID]++
	l.recent[paneID] = append(keep, now)
	return nil
}

// endTurn starts the pane's per-turn count over.
func (l *askLimiter) endTurn(paneID string) {
	l.mu.Lock()
	delete(l.turn, paneID)
	l.mu.Unlock()
}

func (l *askLimiter) forget(paneID string) {
	l.mu.Lock()
	delete(l.turn, paneID)
	delete(l.recent, paneID)
	l.mu.Unlock()
}

func (e *Engine) limiter() *askLimiter {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.asks == nil {
		e.asks = newAskLimiter(time.Now)
	}
	return e.asks
}

// forgetAsks drops a closed pane's ask counters and consultations.
func (e *Engine) forgetAsks(paneID string) {
	e.limiter().forget(paneID)
	e.mu.Lock()
	for k := range e.consulted {
		if k.pane == paneID {
			delete(e.consulted, k)
		}
	}
	e.mu.Unlock()
}

// markConsulted makes the dot skip the turn it was asked in (Decide's
// ReasonConsulted, cleared by processPaneLocked).
func (e *Engine) markConsulted(paneID string, slugs ...string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.d.Attachments.Get(paneID); !ok { // lock order e.mu -> stores
		return false // a closed pane keeps no entries
	}
	if e.consulted == nil {
		e.consulted = map[key]bool{}
	}
	for _, s := range slugs {
		e.consulted[key{paneID, s}] = true
	}
	return true
}

// cancelAsks ends the pane's in-flight asks (its pane closed).
func (e *Engine) cancelAsks(paneID string) {
	e.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(e.askRuns[paneID]))
	for _, c := range e.askRuns[paneID] {
		cancels = append(cancels, c)
	}
	e.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// trackAsk registers an in-flight ask so a closing pane can cancel it.
func (e *Engine) trackAsk(paneID string, cancel context.CancelFunc) (untrack func()) {
	id := new(struct{})
	e.mu.Lock()
	if e.askRuns == nil {
		e.askRuns = map[string]map[*struct{}]context.CancelFunc{}
	}
	if e.askRuns[paneID] == nil {
		e.askRuns[paneID] = map[*struct{}]context.CancelFunc{}
	}
	e.askRuns[paneID][id] = cancel
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		delete(e.askRuns[paneID], id)
		if len(e.askRuns[paneID]) == 0 {
			delete(e.askRuns, paneID)
		}
		e.mu.Unlock()
	}
}

// askable returns the attached dot if it may be asked now: not paused, and
// with call and dollar budget left (Decide's checks without the turn-based
// ones).
func (e *Engine) askable(paneID, slug string) (dots.Dot, error) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return dots.Dot{}, fmt.Errorf("%w '%s'; no dots are attached to this pane (the developer attaches them with prefix+d)", ErrNotAttached, slug)
	}
	da := att.Find(slug)
	if da == nil {
		names := make([]string, 0, len(att.Dots))
		for _, x := range att.Dots {
			names = append(names, x.Slug)
		}
		return dots.Dot{}, fmt.Errorf("%w '%s'; attached: %s", ErrNotAttached, slug, strings.Join(names, ", "))
	}
	e.mu.Lock()
	d, known := e.paneDotLocked(paneID, slug)
	var err error
	if known {
		err = e.askBlockedLocked(paneID, d, da.Paused)
	}
	untrusted := !known && e.untrustedAtLocked(e.paneRoots[paneID], slug)
	e.mu.Unlock()
	if untrusted {
		return dots.Dot{}, fmt.Errorf("%w '%s': %w: the developer reviews and trusts it in the library first", ErrNotAttached, dots.DisplaySlug(slug), ErrUntrusted)
	}
	if !known {
		return dots.Dot{}, fmt.Errorf("%w '%s': its dot file is missing or invalid", ErrNotAttached, dots.DisplaySlug(slug))
	}
	if !d.Capabilities.MCP {
		return d, fmt.Errorf("%w '%s': it is not available to the MCP server (capabilities.mcp = false; imported and project dots start that way)", ErrNotAttached, dots.DisplaySlug(slug))
	}
	return d, err
}

// askBlockedLocked is why the dot can't be asked now, or nil. A dot answers
// one thing at a time (an ask, a turn review or a memory pass), and asks,
// drafts and background runs in flight on the pane count as calls already
// made, so they can't jointly overspend a call budget. A dollar cost is known only after a run,
// so the dollar checks see spend so far; one ask per dot bounds the
// overshoot as for turn reviews. Caller holds e.mu (e.mu -> stores).
func (e *Engine) askBlockedLocked(paneID string, d dots.Dot, paused bool) error {
	k := key{paneID, d.Slug}
	inCouncil := false
	if run := e.councils[paneID]; run != nil {
		for _, m := range run.members {
			inCouncil = inCouncil || m.Slug == d.Slug
		}
	}
	switch {
	case inCouncil:
		return fmt.Errorf("%s is in a council right now; ask again when it ends", d.Name)
	case e.asking[k] || e.running[k]:
		return fmt.Errorf("%s is already answering; ask again when it is done", d.Name)
	case e.pausedAll:
		return fmt.Errorf("all dots are %w", ErrPaused)
	case paused:
		return fmt.Errorf("%s is %w", d.Name, ErrPaused)
	}
	// Asks of the pane's other dots (this one has none: refused above), live
	// drafts and background runs (Phase 8, ruling P8-R2) in flight.
	inFlight := e.inFlightLocked(paneID, key{})
	cfg := e.config()
	if e.d.Usage.Calls(paneID, d.Slug) >= d.Budget.MaxCallsPerSession || OverUSD(e.d.Usage.Cost(paneID, d.Slug), d.Budget.MaxUSDPerSession) {
		return fmt.Errorf("%s: %w for this agent session", d.Name, ErrBudget)
	}
	if e.d.Usage.PaneCalls(paneID)+inFlight >= cfg.SessionBudgetCalls || OverUSD(e.d.Usage.PaneCost(paneID), cfg.SessionBudgetUSD) {
		return fmt.Errorf("the pane's session %w", ErrBudget)
	}
	return nil
}

// reserveAsk re-checks the dot under e.mu, then takes the ask slot and marks
// the dot as being asked in the same critical section: of parallel asks only
// one gets through, and a refused ask uses no slot. release unmarks the dot
// and reviews a turn that ended meanwhile (processPaneLocked deferred it).
func (e *Engine) reserveAsk(paneID string, d dots.Dot) (release func(), err error) {
	k := key{paneID, d.Slug}
	lim, mcp := e.limiter(), e.config().MCP.Normalized()
	e.mu.Lock()
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		e.mu.Unlock()
		return nil, ErrPaneClosed
	}
	paused := false
	if da := att.Find(d.Slug); da != nil {
		paused = da.Paused
	}
	if err := e.askBlockedLocked(paneID, d, paused); err != nil {
		e.mu.Unlock()
		return nil, err
	}
	if err := lim.take(paneID, mcp); err != nil { // a leaf lock
		e.mu.Unlock()
		return nil, err
	}
	if e.asking == nil {
		e.asking = map[key]bool{}
	}
	e.asking[k] = true
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		delete(e.asking, k)
		again := e.rerun[k] && !e.running[k]
		if again {
			delete(e.rerun, k)
		}
		e.mu.Unlock()
		if again {
			e.spawn(func() { e.processPaneIfIdle(paneID) })
		}
	}, nil
}

// Ask runs one dot on the pane now and returns its answer (ask_dot). It is
// a forced run with the dot's memory, backend and budgets; it makes no queue
// card, logs an "asked" feed line, applies the dot's memory ops, and marks
// the dot consulted so it skips the turn it was asked in. The run is tracked
// by Engine.Wait and ends with ctx (the request) or the engine.
func (e *Engine) Ask(ctx context.Context, paneID, slug, question string, includeTurns bool) (ipc.AskResult, error) {
	q := clipAsk(strings.TrimSpace(textutil.SanitizeModelText(question)), maxAskQuestion)
	if q == "" {
		return ipc.AskResult{}, errors.New("the question is empty")
	}
	d, err := e.askable(paneID, slug)
	if err != nil {
		return ipc.AskResult{}, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel) // an engine shutdown ends the ask
	defer stop()
	defer e.trackAsk(paneID, cancel)() // a closing pane ends it too
	// A pane that closed before trackAsk could not cancel this ask: check
	// again now that it can (and never read with an empty attachment).
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return ipc.AskResult{}, ErrPaneClosed
	}
	var turns []transcript.Turn
	if includeTurns {
		ts, err := e.reader(att).Turns(runCtx)
		if err != nil {
			return ipc.AskResult{}, fmt.Errorf("read conversation: %w", err)
		}
		turns = tail(ts, d.Context.Turns)
	}
	// The slot is used only now, together with the dot's reservation: a
	// refusal or a failed read costs none. Only the limits come from [mcp];
	// which tools exist is the mcp command's business, not the engine's.
	release, err := e.reserveAsk(paneID, d)
	if err != nil {
		return ipc.AskResult{}, err
	}
	// The dot stays reserved until both the run has ended (an abandoned run
	// still finishes in the background) and Ask has returned (after
	// markConsulted, so a turn deferred meanwhile sees the consultation).
	var left atomic.Int32
	left.Store(2)
	done := func() {
		if left.Add(-1) == 0 {
			release()
		}
	}
	defer done()
	type outcome struct {
		res backend.Result
		err error
	}
	got := make(chan outcome, 1)
	o := runOpts{summon: true, ask: true, question: askQuestion(q), ctx: runCtx,
		sink: func(res backend.Result, err error) { got <- outcome{res, err}; done() }}
	e.spawn(func() { e.runDot(paneID, d, turns, o) })
	var out outcome
	select {
	case out = <-got:
	case <-runCtx.Done():
		return ipc.AskResult{}, e.askEnded(paneID, d, runCtx)
	}
	// Cancellation first: a run it made fail can win the select above, and
	// that is the ask ending (pane closed, hangup), not the dot failing.
	if runCtx.Err() != nil {
		return ipc.AskResult{}, e.askEnded(paneID, d, runCtx)
	}
	if out.err != nil {
		return ipc.AskResult{}, fmt.Errorf("%s failed: %s", d.Name, errText(errors.New(credentials.Redact(out.err.Error()))))
	}
	if !e.markConsulted(paneID, slug) {
		return ipc.AskResult{}, ErrPaneClosed
	}
	v := out.res.Verdict
	model := out.res.Model
	if model == "" {
		model = d.Backend.Model
	}
	return ipc.AskResult{Slug: slug, Name: d.Name, Relevant: v.Relevant, Severity: v.Severity,
		Message: mcpText(v.Message), CostUSD: out.res.CostUSD,
		CostKnown: out.res.CostKnown, Model: model}, nil
}

// askEnded is the error of an ask whose context ended: the pane closing is
// ErrPaneClosed, anything else (hangup, shutdown) the context's error.
func (e *Engine) askEnded(paneID string, d dots.Dot, runCtx context.Context) error {
	if _, ok := e.d.Attachments.Get(paneID); !ok {
		return ErrPaneClosed
	}
	return fmt.Errorf("%s: %w", d.Name, runCtx.Err())
}

// mcpText is model text on its way to the main agent, which gets it without
// the developer's approval: sanitized, key-shaped text redacted, and every
// line that would parse as a dot header neutralised, as delivery.Format does
// for cards. MCP answers are tool results, which the transcript readers never
// treat as prompts, but a scrollback reader sees the screen.
func mcpText(s string) string {
	return transcript.NeutraliseDotPrefixes(credentials.Redact(strings.TrimSpace(textutil.SanitizeModelText(s))))
}

// askQuestion tells the dot who is asking.
func askQuestion(q string) string {
	return "(The main coding agent is asking you directly, on the developer's behalf, through the Dots MCP server.)\n" + q
}

// askDetail is an "asked" feed line's detail: the severity (or "pass"),
// then the cost detail.
func askDetail(d dots.Dot, res backend.Result) string {
	sev := "pass"
	if res.Verdict.Relevant {
		sev = res.Verdict.Severity
	}
	return withCost(sev, costDetail(d, res))
}

// ListDots is list_dots: the pane's project and its attached dots.
func (e *Engine) ListDots(paneID string) (ipc.DotsResult, error) {
	out := ipc.DotsResult{Dots: []ipc.DotInfo{}}
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return out, nil
	}
	out.ProjectID = e.projectOf(&att) // may run git: outside every engine lock
	out.ProjectName = att.ProjectName
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, da := range att.Dots {
		d, ok := e.paneDotLocked(paneID, da.Slug)
		if !ok {
			d = dots.Dot{Slug: da.Slug, Name: dots.DisplaySlug(da.Slug)}
		}
		st := e.status[key{paneID, da.Slug}]
		if st == "" {
			st = view.StatusIdle
		}
		if da.Paused && st != view.StatusError {
			st = view.StatusPaused
		}
		summary := purposeSummary(d.Purpose)
		if !ok || !d.Capabilities.MCP {
			// Phase 9: ask_dot would refuse it (capabilities.mcp off, or an
			// untrusted project dot): say so, and show none of its text.
			st, summary = dotUnavailable, ""
		}
		out.Dots = append(out.Dots, ipc.DotInfo{Slug: da.Slug, Name: d.Name, PurposeSummary: summary,
			Mode: string(d.Delivery.Mode), Status: st, Paused: da.Paused || e.pausedAll,
			Calls: e.d.Usage.Calls(paneID, da.Slug), Budget: d.Budget.MaxCallsPerSession, CostUSD: e.d.Usage.Cost(paneID, da.Slug)})
	}
	return out, nil
}

// ListQueue is list_queue: pending (and auto) cards, or every card of the
// pane newest first; at most 20, bodies clipped to 2,000 runes.
func (e *Engine) ListQueue(paneID, status string) (ipc.QueueResult, error) {
	var cards []queue.Card
	switch status {
	case "", ipc.QueuePending:
		cards = e.d.Queue.Pending(paneID)
	case ipc.QueueAll:
		for _, c := range e.d.Queue.All() {
			if c.PaneID == paneID {
				cards = append(cards, c)
			}
		}
		// The store keeps finished cards last, so sort by age, newest first.
		slices.SortStableFunc(cards, func(a, b queue.Card) int {
			if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
				return c
			}
			x, _ := strconv.Atoi(a.ID)
			y, _ := strconv.Atoi(b.ID)
			return y - x
		})
	default:
		return ipc.QueueResult{}, fmt.Errorf("status %q: want pending or all", status)
	}
	out := ipc.QueueResult{Cards: []ipc.CardInfo{}, Total: len(cards)}
	for _, c := range cards[:min(len(cards), maxQueueCards)] {
		out.Cards = append(out.Cards, ipc.CardInfo{ID: c.ID, Dot: c.Dot, Severity: c.Severity, Turn: c.Turn,
			Status: string(c.Status), Body: clipAsk(mcpText(c.Body), maxQueueBody)})
	}
	return out, nil
}

// purposeSummary is the purpose's first 200 runes on one line.
func purposeSummary(purpose string) string {
	return clipAsk(strings.Join(strings.Fields(purpose), " "), purposeSummaryChars)
}

// clipAsk cuts s to at most n runes, ending in "…" when cut.
func clipAsk(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:max(0, n-1)]) + "…"
}

var _ ipc.Querier = (*Engine)(nil)
