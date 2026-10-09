package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

type Deps struct {
	Herdr        herdr.Client
	Backend      func(kind string) (backend.Backend, error)
	Reader       func(a state.Attachment) transcript.Reader
	LoadDots     func() ([]dots.Dot, []dots.LoadError)
	SaveDot      func(d dots.Dot) error
	Config       config.Config
	Queue        *queue.Store
	Attachments  *state.Attachments
	Usage        *state.Usage
	Feed         *state.Feed
	Logf         func(format string, args ...any)
	PollInterval time.Duration // 0 → 3s
	MaxFailures  int           // 0 → 3
}

type key struct{ pane, slug string }

type runOpts struct {
	forceQueue bool   // the turn was started by a dot: never auto-inject
	summon     bool   // manual summon: always a card, bypasses running flags
	question   string // summon question
}

// Engine owns all live state. Lock order: e.mu → store mutexes (stores never
// call back). e.dmu serializes deliveries. Backend runs happen outside locks,
// bounded by sem.
type Engine struct {
	d   Deps
	ctx context.Context

	mu        sync.Mutex
	dots      map[string]dots.Dot
	invalid   []dots.LoadError
	readers   map[string]transcript.Reader
	subs      map[string]context.CancelFunc
	running   map[key]bool
	rerun     map[key]bool
	status    map[key]string
	warned    map[key]bool
	pausedAll bool
	watchers  map[int]chan view.Snapshot
	nextW     int

	dmu sync.Mutex
	sem chan struct{}

	paneMu map[string]*sync.Mutex // per-pane; guarded by mu

	// wmu/wg/closed track background goroutines so Wait can drain them.
	wmu    sync.Mutex
	wg     sync.WaitGroup
	closed bool
}

// spawn runs f in a tracked goroutine; after Wait it is a no-op.
func (e *Engine) spawn(f func()) {
	e.wmu.Lock()
	if e.closed {
		e.wmu.Unlock()
		return
	}
	e.wg.Add(1)
	e.wmu.Unlock()
	go func() {
		defer e.wg.Done()
		f()
	}()
}

// Wait blocks until every background goroutine has exited. Cancel the context
// passed to Start first; no new goroutines start once Wait has been called.
func (e *Engine) Wait() {
	e.wmu.Lock()
	e.closed = true
	e.wmu.Unlock()
	e.wg.Wait()
}

func New(d Deps) *Engine {
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	if d.PollInterval <= 0 {
		d.PollInterval = 3 * time.Second
	}
	if d.MaxFailures <= 0 {
		d.MaxFailures = 3
	}
	e := &Engine{
		d:        d,
		ctx:      context.Background(),
		dots:     map[string]dots.Dot{},
		readers:  map[string]transcript.Reader{},
		subs:     map[string]context.CancelFunc{},
		running:  map[key]bool{},
		rerun:    map[key]bool{},
		status:   map[key]string{},
		warned:   map[key]bool{},
		watchers: map[int]chan view.Snapshot{},
		paneMu:   map[string]*sync.Mutex{},
		sem:      make(chan struct{}, max(1, d.Config.MaxParallelRuns)),
	}
	e.ReloadDots()
	return e
}

// paneLock returns the per-pane mutex that serializes checkSession, Attach and
// processPane (transcript read through LastSeenTurn update) for one pane.
// Lock order: paneLock -> e.mu -> store mutexes. e.dmu is taken independently
// of paneLock; under dmu the engine only takes e.mu (via broadcast) and stores.
// Never hold a paneLock while calling the backend.
func (e *Engine) paneLock(paneID string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	m, ok := e.paneMu[paneID]
	if !ok {
		m = &sync.Mutex{}
		e.paneMu[paneID] = m
	}
	return m
}

func (e *Engine) ReloadDots() {
	list, invalid := e.d.LoadDots()
	m := make(map[string]dots.Dot, len(list))
	for _, d := range list {
		m[d.Slug] = d
	}
	e.mu.Lock()
	e.dots, e.invalid = m, invalid
	e.mu.Unlock()
	e.broadcast()
}

// Start restores persisted attachments (dropping panes that no longer exist)
// and begins polling. Call once, before serving commands.
func (e *Engine) Start(ctx context.Context) {
	e.ctx = ctx
	for _, a := range e.d.Attachments.List() {
		if _, err := e.d.Herdr.PaneGet(ctx, a.PaneID); err != nil {
			if herdr.IsNotFound(err) {
				e.dropPane(a.PaneID)
				continue
			}
			// Transient failure (slow socket, timeout): keep the attachment;
			// poll drops the pane later if it is really gone.
			e.d.Logf("pane %s: start check failed (%v); keeping attachment", a.PaneID, err)
		}
		e.subscribe(a.PaneID)
	}
	e.spawn(e.pollLoop)
}

func (e *Engine) Attach(ctx context.Context, paneID string, slugs []string) error {
	p, err := e.d.Herdr.PaneGet(ctx, paneID)
	if err != nil {
		return fmt.Errorf("pane %s: %w", paneID, err)
	}
	if p.Agent == "" {
		return fmt.Errorf("pane %s has no coding agent: dots attach to agent panes only", paneID)
	}
	pl := e.paneLock(paneID)
	pl.Lock()
	defer pl.Unlock()
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		att = state.Attachment{PaneID: paneID, Agent: p.Agent, Cwd: p.WorkDir()}
		if p.AgentSession != nil {
			att.SessionKind, att.SessionValue = p.AgentSession.Kind, p.AgentSession.Value
		}
	}
	r := e.reader(att)
	turns, err := r.Turns(ctx)
	if err != nil {
		e.d.Logf("pane %s: initial transcript read: %v", paneID, err)
	}
	e.mu.Lock()
	add := make([]state.DotAttachment, 0, len(slugs))
	for _, slug := range slugs {
		d, ok := e.dots[slug]
		if !ok {
			e.mu.Unlock()
			return fmt.Errorf("unknown dot %q", slug)
		}
		// Catch-up: the newest Context.Turns turns count as "new" for this dot.
		add = append(add, state.DotAttachment{Slug: slug, LastSeenTurn: max(0, len(turns)-d.Context.Turns)})
	}
	// Update-or-create in one store call: runFailed, cooldown or Detach updates
	// made while the transcript was read are kept, not overwritten.
	err = e.d.Attachments.Upsert(att, func(x *state.Attachment) {
		x.Source = r.Source()
		for _, da := range add {
			if x.Find(da.Slug) == nil {
				x.Dots = append(x.Dots, da)
				delete(e.warned, key{paneID, da.Slug})
			}
		}
	})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	e.subscribe(paneID)
	e.spawn(func() { e.processPane(paneID) })
	e.broadcast()
	return nil
}

func (e *Engine) Detach(paneID, slug string) error {
	empty := false
	err := e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		kept := a.Dots[:0]
		for _, da := range a.Dots {
			if da.Slug != slug {
				kept = append(kept, da)
			}
		}
		a.Dots = kept
		empty = len(kept) == 0
	})
	if err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.status, key{paneID, slug})
	delete(e.warned, key{paneID, slug})
	e.mu.Unlock()
	if empty {
		e.unwatch(paneID)
		_ = e.d.Attachments.Delete(paneID)
	}
	e.broadcast()
	return nil
}

func (e *Engine) reader(att state.Attachment) transcript.Reader {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r, ok := e.readers[att.PaneID]; ok {
		return r
	}
	r := e.d.Reader(att)
	e.readers[att.PaneID] = r
	return r
}

func (e *Engine) dropReader(paneID string) {
	e.mu.Lock()
	delete(e.readers, paneID)
	e.mu.Unlock()
}

func (e *Engine) subscribe(paneID string) {
	e.mu.Lock()
	if _, ok := e.subs[paneID]; ok {
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	e.subs[paneID] = cancel
	e.mu.Unlock()
	e.spawn(func() { e.watchPane(ctx, paneID) })
}

func (e *Engine) unwatch(paneID string) {
	e.mu.Lock()
	if cancel, ok := e.subs[paneID]; ok {
		cancel()
		delete(e.subs, paneID)
	}
	delete(e.readers, paneID)
	e.mu.Unlock()
}

// watchPane turns working→idle transitions into (debounced) turn processing,
// resubscribing if the stream drops.
func (e *Engine) watchPane(ctx context.Context, paneID string) {
	for ctx.Err() == nil {
		ch, err := e.d.Herdr.Subscribe(ctx, paneID)
		if err != nil {
			e.d.Logf("pane %s: subscribe: %v", paneID, err)
			sleep(ctx, 2*time.Second)
			continue
		}
		var timer *time.Timer
		for ev := range ch {
			if timer != nil {
				timer.Stop()
			}
			if ev.Status == herdr.StatusIdle || ev.Status == herdr.StatusDone {
				timer = time.AfterFunc(e.d.Config.Debounce(), func() { e.spawn(func() { e.onIdle(paneID) }) })
			}
		}
		if timer != nil {
			timer.Stop()
		}
		sleep(ctx, time.Second)
	}
}

func (e *Engine) onIdle(paneID string) {
	p, err := e.d.Herdr.PaneGet(e.ctx, paneID)
	if err != nil || (p.AgentStatus != herdr.StatusIdle && p.AgentStatus != herdr.StatusDone) {
		return
	}
	if a, ok := e.d.Attachments.Get(paneID); ok {
		e.checkSession(a, p)
	}
	e.processPane(paneID)
	e.flushAuto(paneID)
}

type job struct {
	dot        dots.Dot
	turns      []transcript.Turn
	forceQueue bool
}

// processPane reads the transcript and starts a run for every dot whose
// filter passes on the turns it hasn't seen.
func (e *Engine) processPane(paneID string) {
	pl := e.paneLock(paneID)
	pl.Lock()
	jobs, budgetHit := e.processPaneLocked(paneID)
	pl.Unlock()

	for _, d := range budgetHit {
		e.notify(d, "budget reached; resets on a new agent session or press r")
	}
	for _, j := range jobs {
		e.spawn(func() { e.runDot(paneID, j.dot, j.turns, runOpts{forceQueue: j.forceQueue}) })
	}
	e.broadcast()
}

// processPaneLocked runs with the pane lock held.
func (e *Engine) processPaneLocked(paneID string) ([]job, []dots.Dot) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return nil, nil
	}
	session := att.SessionValue
	turns, err := e.reader(att).Turns(e.ctx)
	if err != nil && !att.ForceScrollback {
		e.d.Logf("pane %s: transcript read failed (%v); falling back to scrollback", paneID, err)
		_ = e.d.Attachments.Update(paneID, func(a *state.Attachment) {
			a.ForceScrollback, a.Source = true, transcript.SourceScrollback
		})
		e.dropReader(paneID)
		att, _ = e.d.Attachments.Get(paneID)
		turns, err = e.reader(att).Turns(e.ctx)
	}
	if err != nil {
		e.d.Logf("pane %s: read: %v", paneID, err)
		return nil, nil
	}

	var jobs []job
	var budgetHit []dots.Dot
	e.mu.Lock()
	paneCalls := e.d.Usage.PaneCalls(paneID)
	_ = e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		if a.SessionValue != session {
			return // the session changed under us; this read is stale
		}
		for i := range a.Dots {
			da := &a.Dots[i]
			d, ok := e.dots[da.Slug]
			if !ok {
				continue
			}
			start := min(da.LastSeenTurn, len(turns))
			newTurns := turns[start:]
			if len(newTurns) == 0 {
				da.LastSeenTurn = len(turns)
				continue
			}
			k := key{paneID, da.Slug}
			if e.running[k] {
				e.rerun[k] = true // pick these turns up when the current run ends
				continue
			}
			dec := Decide(FilterInput{
				Dot: d, Att: *da, NewTurns: newTurns, PausedAll: e.pausedAll,
				DotCalls: e.d.Usage.Calls(paneID, da.Slug), PaneCalls: paneCalls,
				PaneBudget: e.d.Config.SessionBudgetCalls,
			})
			da.LastSeenTurn = len(turns)
			switch dec.Reason {
			case ReasonCooldown:
				da.CooldownLeft--
			case ReasonBudget, ReasonSessionBudget:
				e.status[k] = view.StatusBudget
				if !e.warned[k] {
					e.warned[k] = true
					budgetHit = append(budgetHit, d)
				}
			}
			if !dec.Run {
				continue
			}
			e.running[k] = true
			paneCalls++
			jobs = append(jobs, job{dot: d, turns: tail(turns, d.Context.Turns), forceQueue: anyFromDot(newTurns)})
		}
	})
	e.mu.Unlock()

	return jobs, budgetHit
}

func (e *Engine) runDot(paneID string, d dots.Dot, turns []transcript.Turn, o runOpts) {
	k := key{paneID, d.Slug}
	defer func() {
		if !o.summon {
			e.finishRun(k)
		} else {
			e.broadcast()
		}
	}()
	select {
	case e.sem <- struct{}{}:
	case <-e.ctx.Done():
		return
	}
	defer func() { <-e.sem }()

	e.setStatus(k, view.StatusThinking)
	att, _ := e.d.Attachments.Get(paneID)
	turn := 0
	if len(turns) > 0 {
		turn = turns[len(turns)-1].Index
	}
	be, err := e.d.Backend(d.Backend.Kind)
	if err != nil {
		e.runFailed(k, d, turn, err)
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.d.Config.RunTimeout())
	defer cancel()
	res, err := be.Run(ctx, backend.Request{
		DotName: d.Name, Purpose: d.Purpose, Verbosity: d.Context.Verbosity,
		Model: d.Backend.Model, Args: d.Backend.Args, RepoRead: d.Context.RepoRead,
		Cwd: att.Cwd, Turns: turns, Question: o.question, Force: o.summon,
	})
	_ = e.d.Usage.Record(paneID, d.Slug, res.InputTokens, res.OutputTokens)
	if err != nil {
		e.runFailed(k, d, turn, err)
		return
	}
	_ = e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		if da := a.Find(d.Slug); da != nil {
			da.Failures = 0
			if res.Verdict.Relevant {
				da.CooldownLeft = d.Trigger.Cooldown
			}
		}
	})
	if !res.Verdict.Relevant {
		e.feed(paneID, d.Slug, turn, state.OutcomePass, "")
		e.setStatus(k, view.StatusPassed)
		return
	}
	e.feed(paneID, d.Slug, turn, state.OutcomeFlagged, res.Verdict.Message)
	e.setStatus(k, view.StatusFlagged)
	e.route(paneID, d, turn, res.Verdict, o)
}

func (e *Engine) runFailed(k key, d dots.Dot, turn int, err error) {
	e.d.Logf("dot %s on %s: %v", d.Slug, k.pane, err)
	e.feed(k.pane, d.Slug, turn, state.OutcomeError, err.Error())
	e.setStatus(k, view.StatusError)
	stopped := false
	_ = e.d.Attachments.Update(k.pane, func(a *state.Attachment) {
		if da := a.Find(d.Slug); da != nil {
			da.Failures++
			if da.Failures >= e.d.MaxFailures && !da.Paused {
				da.Paused, stopped = true, true
			}
		}
	})
	if stopped {
		e.notify(d, fmt.Sprintf("stopped after %d failures: %s (press r in the Dots pane to reset)", e.d.MaxFailures, err))
	}
}

func (e *Engine) finishRun(k key) {
	e.mu.Lock()
	delete(e.running, k)
	again := e.rerun[k]
	delete(e.rerun, k)
	e.mu.Unlock()
	e.broadcast()
	if again {
		e.spawn(func() { e.processPane(k.pane) })
	}
}

// route turns a relevant verdict into a card according to the dot's mode.
func (e *Engine) route(paneID string, d dots.Dot, turn int, v backend.Verdict, o runOpts) {
	mode := d.Delivery.Mode
	if o.summon || mode == dots.ModeObserve {
		mode = dots.ModeQueue
	}
	if mode == dots.ModeInject && (o.forceQueue || !e.d.Config.InjectEnabled) {
		mode = dots.ModeQueue
	}
	c := queue.Card{Dot: d.Slug, DotName: d.Name, Glyph: d.Glyph, PaneID: paneID, Turn: turn,
		Severity: v.Severity, Body: v.Message, Mode: string(mode)}
	if mode == dots.ModeInject {
		c.Status = queue.StatusAuto
	}
	if _, err := e.d.Queue.Add(c); err != nil {
		e.d.Logf("queue add: %v", err)
		return
	}
	if mode == dots.ModeSuggest && (v.Severity == "warn" || v.Severity == "critical") {
		e.notify(d, firstLine(v.Message))
	}
	e.reportPending(paneID)
	e.broadcast()
	if mode == dots.ModeInject {
		e.flushAuto(paneID)
	}
}

func (e *Engine) setStatus(k key, s string) {
	e.mu.Lock()
	e.status[k] = s
	e.mu.Unlock()
	e.broadcast()
}

func (e *Engine) notify(d dots.Dot, body string) {
	if err := e.d.Herdr.Notify(e.ctx, d.Glyph+" "+d.Name+" dot", body); err != nil {
		e.d.Logf("notify: %v", err)
	}
}

func (e *Engine) feed(paneID, slug string, turn int, outcome, detail string) {
	if err := e.d.Feed.Append(state.FeedEntry{Time: time.Now(), PaneID: paneID, Dot: slug, Turn: turn, Outcome: outcome, Detail: detail}); err != nil {
		e.d.Logf("feed: %v", err)
	}
}

func (e *Engine) Snapshot() view.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := view.Snapshot{InjectEnabled: e.d.Config.InjectEnabled, PausedAll: e.pausedAll}
	slugs := make([]string, 0, len(e.dots))
	for slug := range e.dots {
		slugs = append(slugs, slug)
	}
	sort.Slice(slugs, func(i, j int) bool {
		return strings.ToLower(e.dots[slugs[i]].Name) < strings.ToLower(e.dots[slugs[j]].Name)
	})
	for _, slug := range slugs {
		d := e.dots[slug]
		s.Library = append(s.Library, view.LibraryDot{Slug: slug, Name: d.Name, Glyph: d.Glyph, Color: d.Color,
			Backend: d.Backend.Kind, Model: d.Backend.Model, Mode: string(d.Delivery.Mode)})
	}
	for _, le := range e.invalid {
		s.Invalid = append(s.Invalid, view.Invalid{File: le.File, Line: le.Line, Error: le.Err.Error()})
	}
	seen := map[string]bool{}
	for _, a := range e.d.Attachments.List() {
		pv := view.Pane{PaneID: a.PaneID, Agent: a.Agent, Source: a.Source,
			Calls: e.d.Usage.PaneCalls(a.PaneID), Budget: e.d.Config.SessionBudgetCalls}
		for _, da := range a.Dots {
			d, ok := e.dots[da.Slug]
			if !ok {
				d = dots.Dot{Slug: da.Slug, Name: da.Slug, Glyph: "?"}
			}
			st := e.status[key{a.PaneID, da.Slug}]
			if st == "" {
				st = view.StatusIdle
			}
			if da.Paused && st != view.StatusError {
				st = view.StatusPaused
			}
			pv.Dots = append(pv.Dots, view.DotState{Slug: da.Slug, Name: d.Name, Glyph: d.Glyph, Color: d.Color,
				Mode: string(d.Delivery.Mode), Status: st, Paused: da.Paused,
				Calls: e.d.Usage.Calls(a.PaneID, da.Slug), Budget: d.Budget.MaxCallsPerSession})
		}
		pv.Cards = e.d.Queue.Visible(a.PaneID)
		pv.Feed = e.d.Feed.Recent(a.PaneID, 20)
		s.Panes = append(s.Panes, pv)
		seen[a.PaneID] = true
	}
	for _, c := range e.d.Queue.All() {
		if seen[c.PaneID] || (c.Status != queue.StatusPending && c.Status != queue.StatusAuto && c.Status != queue.StatusOrphaned) {
			continue
		}
		seen[c.PaneID] = true
		s.Panes = append(s.Panes, view.Pane{PaneID: c.PaneID, Cards: e.d.Queue.Visible(c.PaneID), Feed: e.d.Feed.Recent(c.PaneID, 20)})
	}
	return s
}

// Watch returns a channel that always holds the latest snapshot (buffer 1).
func (e *Engine) Watch() (<-chan view.Snapshot, func()) {
	ch := make(chan view.Snapshot, 1)
	e.mu.Lock()
	id := e.nextW
	e.nextW++
	e.watchers[id] = ch
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.watchers, id)
		e.mu.Unlock()
	}
}

func (e *Engine) broadcast() {
	s := e.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ch := range e.watchers {
		select {
		case <-ch: // drop the stale snapshot
		default:
		}
		select {
		case ch <- s:
		default:
		}
	}
}

func tail(turns []transcript.Turn, n int) []transcript.Turn {
	if len(turns) > n {
		return turns[len(turns)-n:]
	}
	return turns
}

func anyFromDot(turns []transcript.Turn) bool {
	for _, t := range turns {
		if t.FromAnyDot() {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(line); len(r) > 140 {
		return string(r[:140]) + "…"
	}
	return line
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
