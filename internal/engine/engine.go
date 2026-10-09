package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
	"herdr-dots/internal/worktree"
)

type Deps struct {
	Herdr        herdr.Client
	Backend      func(kind string) (backend.Backend, error)
	Reader       func(a state.Attachment) transcript.Reader
	LoadDots     func() ([]dots.Dot, []dots.LoadError)
	SaveDot      func(d dots.Dot) error
	Config       config.Config // initial settings
	ConfigPath   string        // config.toml, re-read when it changes; "" = never
	ConfigCheck  time.Duration // how often poll stats ConfigPath; 0 → 3s
	Queue        *queue.Store
	Attachments  *state.Attachments
	Usage        *state.Usage
	Feed         *state.Feed
	Logf         func(format string, args ...any)
	PollInterval time.Duration // 0 → 3s
	MaxFailures  int           // 0 → 3

	Memory         *memory.Store                            // nil = memory off (v1 behavior)
	ResolveProject func(cwd string) (memory.Project, error) // nil = memory.ResolveProject with git

	// Proactive dots (Phase 8). A nil Daily turns research, schedules and
	// commit triggers off; a nil Inbox drops results for unattached projects.
	Clock func() time.Time // nil = time.Now; every proactive timer reads it
	Daily *state.Daily     // STATE_DIR/daily.json, shared by every session
	Inbox *queue.Inbox     // STATE_DIR/inbox.json, shared by every session

	// Drafts (Phase 7). Patches nil = drafts unavailable.
	Patches      *patchstore.Store // SessionDir/patches
	WorktreesDir string            // SessionDir/worktrees
	Git          worktree.Git      // hooks off, timeouts
}

type key struct{ pane, slug string }

type runOpts struct {
	origin string // Phase 8: a background card's origin (state.RunCommit) and header label
	label  string
	ask    bool // MCP ask_dot (ask.go): feed "asked", status consulted, no card, no cooldown

	// Council members (council.go) only.
	noForce bool                        // summon semantics, but the member may pass ("nothing from my side")
	ctx     context.Context             // cancels the run with its council; nil = the engine's context
	sink    func(backend.Result, error) // receives the result instead of route; called exactly once
	noLearn bool                        // round 2: the run reads other members' text, so it never learns

	forceQueue bool   // the turn was started by a dot: never auto-inject
	summon     bool   // manual summon: always a card, bypasses running flags
	question   string // summon question
	memoryPass bool   // observe dot's memory-only pass: never a card
	seenTo     int    // memory pass: MemorySeenTurn once it succeeds
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
	paid      map[key]bool // running reviews whose charge has landed: Usage counts them, so inFlightLocked must not
	rerun     map[key]bool
	status    map[key]string
	warned    map[key]bool
	pausedAll bool
	watchers  map[int]chan view.Snapshot
	nextW     int

	dmu sync.Mutex
	sem chan struct{}

	cfg         atomic.Pointer[config.Config] // current settings; read via e.config()
	cfgMu       sync.Mutex                    // leaf lock: guards cfgStamp, cfgChecked, chairWarned
	cfgStamp    string
	cfgChecked  time.Time
	chairWarned bool // the "chair needs a model" line was logged for the current settings

	paneMu map[string]*sync.Mutex // per-pane; guarded by mu

	// MCP state (ask.go), guarded by mu; created lazily.
	consulted map[key]bool                                // asked since the pane's last processed turn
	asks      *askLimiter                                 // per-pane ask limits; its own leaf lock
	askRuns   map[string]map[*struct{}]context.CancelFunc // in-flight asks by pane
	asking    map[key]bool                                // dots with an ask in flight (one per dot)

	// Council state (council.go), guarded by mu; maps are created lazily.
	councils     map[string]*councilRun       // by pane: the council in progress
	preflights   map[string]council.Preflight // by pane: the last preflight, for snapshots
	councilSeq   int
	preflightSeq uint64        // stamps every preflight (council.Preflight.Seq)
	councilWait  time.Duration // tests only: deadline for all member runs; 0 = run timeout × rounds

	// Memory state, guarded by mu. Memory I/O never happens under mu.
	learning map[key]bool            // a memory pass is running
	memStats map[string]memory.Stats // by project id, for snapshots
	origBody map[string]string       // card id → body before its first edit
	memToast bool                    // "memory write failed" already shown

	// Drafts (draft.go), guarded by mu. Git and patch I/O never happen under mu.
	drafts    map[string]*draftRun // running drafts by id
	applying  map[string]bool      // patch cards being applied or discarded, by card id
	gens      map[string]uint64    // by pane id: bumped whenever the attachment is dropped, so a reused id is a new attachment
	startedAt time.Time            // Start time: older "drafting" records were interrupted

	cleanupStop func(step string) bool // tests only: true = the process "dies" before that cleanup step
	testHook    func(point string)     // tests only: called at named points of apply and discard

	// Proactive state (Phase 8), guarded by mu. Daily, inbox, memory and git
	// I/O never happen under mu.
	bgBusy   map[key]string                                                // the running background job (it holds bgSem) -> its kind
	bgQueued map[key]int                                                   // background jobs queued or running per key, for dedupe
	bgSem    chan struct{}                                                 // one background run at a time per engine
	today    state.Day                                                     // last daily.json read (snapshot and roster hints)
	todayAt  time.Time                                                     // when today was read
	dailyErr string                                                        // the local date a run's cost could not be recorded: no more background work that day
	inboxAt  time.Time                                                     // last inbox sweep
	toasted  map[string]string                                             // once-a-day toasts: key -> date
	idle     map[string]*idleState                                         // pane -> idle research bookkeeping (idle.go)
	plan     map[string]time.Time                                          // scheduled dot -> its next slot (schedule.go)
	schedOff bool                                                          // a schedule tick found proactive work or memory off (schedule.go)
	dailyAdd func(slug, kind string, costUSD float64, now time.Time) error // tests only: replaces Daily.Add
	sleep    func(time.Duration)                                           // tests only: replaces the Daily.Add retry backoff's wait

	heads       map[key]headSeen // on_commit dot on a pane -> the HEAD it last saw (commits.go)
	commitAt    time.Time        // last HEAD check
	commitBusy  bool             // a HEAD check is running
	commitAgain bool             // a HEAD check skipped a dot whose review was queued: check again when it ends
	commitErrs  map[string]bool  // repo dir + git error already logged this session (commits.go); created lazily

	// wmu/wg/closed track background goroutines so Wait can drain them.
	wmu    sync.Mutex
	wg     sync.WaitGroup
	closed bool
}

// spawn runs f in a tracked goroutine; after Wait it is a no-op. It reports
// whether f was started.
func (e *Engine) spawn(f func()) bool {
	e.wmu.Lock()
	if e.closed {
		e.wmu.Unlock()
		return false
	}
	e.wg.Add(1)
	e.wmu.Unlock()
	go func() {
		defer e.wg.Done()
		f()
	}()
	return true
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
	if d.Memory != nil && d.ResolveProject == nil {
		d.ResolveProject = func(cwd string) (memory.Project, error) {
			return memory.ResolveProject(context.Background(), cwd, memory.ExecGit)
		}
	}
	e := &Engine{
		d:        d,
		ctx:      context.Background(),
		dots:     map[string]dots.Dot{},
		readers:  map[string]transcript.Reader{},
		subs:     map[string]context.CancelFunc{},
		running:  map[key]bool{},
		paid:     map[key]bool{},
		rerun:    map[key]bool{},
		status:   map[key]string{},
		warned:   map[key]bool{},
		watchers: map[int]chan view.Snapshot{},
		paneMu:   map[string]*sync.Mutex{},
		sem:      make(chan struct{}, max(1, d.Config.MaxParallelRuns)),
		learning: map[key]bool{},
		memStats: map[string]memory.Stats{},
		origBody: map[string]string{},
		bgBusy:   map[key]string{},
		bgQueued: map[key]int{},
		bgSem:    make(chan struct{}, 1),
		toasted:  map[string]string{},
		idle:     map[string]*idleState{},
		plan:     map[string]time.Time{},
		heads:    map[key]headSeen{},
		drafts:   map[string]*draftRun{},
		applying: map[string]bool{},
		gens:     map[string]uint64{},
	}
	if e.d.ConfigCheck <= 0 {
		e.d.ConfigCheck = 3 * time.Second
	}
	cfg := d.Config
	e.cfg.Store(&cfg)
	e.cfgStamp = configStamp(d.ConfigPath)
	e.noteChairConfig(cfg)
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
	e.plan = map[string]time.Time{} // re-plan every schedule from the reloaded dots
	e.mu.Unlock()
	e.broadcast()
}

// Start restores persisted attachments (dropping panes that no longer exist)
// and begins polling. Call once, before serving commands.
func (e *Engine) Start(ctx context.Context) {
	e.ctx = ctx
	e.mu.Lock()
	e.startedAt = time.Now()
	e.mu.Unlock()
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
	if e.d.Memory != nil {
		e.spawn(e.memoryLoop)
	}
	if e.d.Patches != nil {
		e.spawn(e.draftLoop)
	}
}

func (e *Engine) Attach(ctx context.Context, paneID string, slugs []string) error {
	p, err := e.d.Herdr.PaneGet(ctx, paneID)
	if err != nil {
		return fmt.Errorf("pane %s: %w", paneID, err)
	}
	if p.Agent == "" {
		return fmt.Errorf("pane %s has no coding agent: dots attach to agent panes only", paneID)
	}
	// Resolve the memory project before taking the pane lock (it runs git).
	// With memory_enabled = false nothing runs and nothing is recorded.
	var pr memory.Project
	if prev, ok := e.d.Attachments.Get(paneID); e.memEnabled() && (!ok || prev.ProjectID == "") {
		pr = e.resolve(p.WorkDir())
	}
	pl := e.paneLock(paneID)
	pl.Lock()
	defer pl.Unlock()
	// The Upsert base comes from the live pane with no dots, never from the
	// stored attachment: a Detach of the last dot during the read below deletes
	// the attachment, and a base copied from it would bring that dot back.
	base := state.Attachment{PaneID: paneID, Agent: p.Agent, Cwd: p.WorkDir()}
	if p.AgentSession != nil {
		base.SessionKind, base.SessionValue = p.AgentSession.Kind, p.AgentSession.Value
	}
	base.ProjectID, base.ProjectName = pr.ID, pr.Name
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		att = base
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
		// Catch-up: the newest Context.Turns turns count as "new" for this dot
		// (memory passes start counting from the same place).
		seen := max(0, len(turns)-d.Context.Turns)
		add = append(add, state.DotAttachment{Slug: slug, LastSeenTurn: seen, MemorySeenTurn: seen})
	}
	// Update-or-create in one store call: runFailed, cooldown or Detach updates
	// made while the transcript was read are kept, not overwritten.
	err = e.d.Attachments.Upsert(base, func(x *state.Attachment) {
		x.Source = r.Source()
		if x.ProjectID == "" && pr.ID != "" {
			x.ProjectID, x.ProjectName = pr.ID, pr.Name
		}
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
	if pr.ID != "" {
		e.spawn(func() { e.seeProject(pr) })
	}
	if a, ok := e.d.Attachments.Get(paneID); ok && a.ProjectID != "" {
		pid := a.ProjectID
		e.spawn(func() { e.claimInbox(paneID, pid) })  // scheduled results waiting for this project
		e.spawn(func() { e.seeDots(pid, slugsOf(a)) }) // for schedules with projects = ["attached"]
	}
	e.subscribe(paneID)
	e.spawn(func() { e.processPaneIfIdle(paneID) })
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
	e.hook("detach-updated")
	var cr *councilRun // the pane's council, abandoned with the attachment
	e.mu.Lock()
	delete(e.status, key{paneID, slug})
	delete(e.warned, key{paneID, slug})
	e.abandonDraftsLocked(paneID, slug) // a detached dot's draft would make a card for a dot that is gone
	if empty {
		// Decided again here: an Attach (which upserts under e.mu) may have
		// added a dot since the update above.
		cur, ok := e.d.Attachments.Get(paneID)
		empty = ok && len(cur.Dots) == 0
	}
	orphaned := 0
	var oerr error
	if empty {
		// The attachment ends as in dropPane, in the same e.mu section as the
		// delete: a draft registering after it sees the attachment gone, one
		// before it is abandoned here; the pane's pending cards are orphaned
		// and its usage reset, so the same pane id attached again inherits no
		// card (a digest included), spend or status.
		e.abandonDraftsLocked(paneID, "")
		if run := e.councils[paneID]; run != nil { // a closed pane gets no council card
			e.abandonRunLocked(run)
			cr = run
		}
		delete(e.preflights, paneID)
		e.gens[paneID]++
		delete(e.idle, paneID) // a later attach starts a fresh idle period (idle.go)
		orphaned, oerr = e.d.Queue.OrphanPane(paneID)
		_ = e.d.Usage.ResetPane(paneID)
		_ = e.d.Attachments.Delete(paneID)
		for k := range e.status {
			if k.pane == paneID {
				delete(e.status, k)
			}
		}
	}
	e.mu.Unlock()
	if cr != nil {
		cr.cancel()
	}
	if empty {
		if oerr != nil {
			e.d.Logf("orphan cards for %s: %v", paneID, oerr)
		} else if orphaned > 0 {
			e.d.Logf("pane %s: last dot detached: %d card(s) orphaned", paneID, orphaned)
		}
		e.unwatch(paneID)
		// After the attachment is gone, as in dropPane: in-flight MCP asks
		// end with ErrPaneClosed, charge nothing, and nothing re-marks the pane.
		e.cancelAsks(paneID)
		e.forgetAsks(paneID)
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
				timer = time.AfterFunc(e.config().Debounce(), func() { e.spawn(func() { e.onIdle(paneID) }) })
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

// processPaneIfIdle is processPane for callers that are not onIdle (a rerun
// after a run, Attach's catch-up). If the agent is working, the transcript's
// last turn may be half written, so it does nothing: LastSeenTurn hasn't
// moved, and the next idle/done event reviews the finished turn.
func (e *Engine) processPaneIfIdle(paneID string) {
	p, err := e.d.Herdr.PaneGet(e.ctx, paneID)
	if err != nil || (p.AgentStatus != herdr.StatusIdle && p.AgentStatus != herdr.StatusDone) {
		return
	}
	e.processPane(paneID)
}

type job struct {
	dot        dots.Dot
	turns      []transcript.Turn
	forceQueue bool
	memoryPass bool
	seenTo     int
}

// processPane reads the transcript and starts a run for every dot whose
// filter passes on the turns it hasn't seen.
func (e *Engine) processPane(paneID string) {
	pl := e.paneLock(paneID)
	pl.Lock()
	jobs, budgetHit := e.processPaneLocked(paneID)
	pl.Unlock()

	for _, b := range budgetHit {
		e.notify(b.dot, b.msg)
	}
	for _, j := range jobs {
		e.spawn(func() {
			e.runDot(paneID, j.dot, j.turns, runOpts{forceQueue: j.forceQueue, memoryPass: j.memoryPass, seenTo: j.seenTo})
		})
	}
	e.broadcast()
}

// budgetNote is a budget toast to send once the pane lock is released.
type budgetNote struct {
	dot dots.Dot
	msg string
}

// processPaneLocked runs with the pane lock held.
func (e *Engine) processPaneLocked(paneID string) ([]job, []budgetNote) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return nil, nil
	}
	session := att.SessionValue
	att = e.upgradeFromScrollback(att)
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
	var budgetHit []budgetNote
	turnEnded := false
	e.mu.Lock()
	// Reviews, memory passes, asks, drafts and background runs in flight
	// count as calls already made (ruling P8-R8, the shared inFlightLocked),
	// and so does each run this pass starts (paneCalls++ below).
	paneCalls := e.d.Usage.PaneCalls(paneID) + e.inFlightLocked(paneID, key{})
	paneCost := e.d.Usage.PaneCost(paneID)
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
			if da.MemorySeenTurn > len(turns) {
				da.MemorySeenTurn = len(turns) // follow a replaced, shorter reader down
			}
			if len(newTurns) == 0 {
				da.LastSeenTurn = len(turns)
				continue
			}
			turnEnded = true
			k := key{paneID, da.Slug}
			if e.running[k] || e.asking[k] {
				// Pick these turns up when the current run or ask ends (an
				// ask that answers consults the dot, so the turn costs no
				// second run).
				e.rerun[k] = true
				continue
			}
			consulted := e.consulted[k]
			delete(e.consulted, k) // a consultation covers the turn it happened in, once
			in := FilterInput{
				Dot: d, Att: *da, NewTurns: newTurns, PausedAll: e.pausedAll, Consulted: consulted,
				DotCalls: e.d.Usage.Calls(paneID, da.Slug), PaneCalls: paneCalls,
				PaneBudget: e.config().SessionBudgetCalls,
				DotCostUSD: e.d.Usage.Cost(paneID, da.Slug), PaneCostUSD: paneCost,
				DotBudgetUSD: d.Budget.MaxUSDPerSession, PaneBudgetUSD: e.config().SessionBudgetUSD,
			}
			dec := Decide(in)
			da.LastSeenTurn = len(turns)
			if e.status[k] == view.StatusConsulted && dec.Reason != ReasonConsulted {
				e.status[k] = view.StatusIdle // the pane's next turn ends the consultation
			}
			switch dec.Reason {
			case ReasonCooldown:
				da.CooldownLeft--
			case ReasonConsulted:
				e.status[k] = view.StatusConsulted
			case ReasonBudget, ReasonSessionBudget:
				e.status[k] = view.StatusBudget
				if !e.warned[k] {
					e.warned[k] = true
					budgetHit = append(budgetHit, budgetNote{dot: d, msg: budgetMessage(in, dec.Reason)})
				}
			}
			if dec.Reason == ReasonObserve && e.memOn(d) {
				memTurns := turns[da.MemorySeenTurn:]
				if DecideMemory(MemoryInput{
					Dot: d, Att: *da, NewTurns: memTurns, PausedAll: e.pausedAll, MemoryEnabled: e.config().MemoryEnabled,
					DotCalls: e.d.Usage.Calls(paneID, da.Slug), PaneCalls: paneCalls, PaneBudget: e.config().SessionBudgetCalls,
					DotCostUSD: e.d.Usage.Cost(paneID, da.Slug), PaneCostUSD: paneCost,
					DotBudgetUSD: d.Budget.MaxUSDPerSession, PaneBudgetUSD: e.config().SessionBudgetUSD,
				}) {
					e.running[k] = true
					delete(e.paid, k)
					paneCalls++
					jobs = append(jobs, job{dot: d, turns: tail(memTurns, min(20, 2*d.Context.Turns)), memoryPass: true, seenTo: len(turns)})
				}
				continue
			}
			if !dec.Run {
				continue
			}
			e.running[k] = true
			delete(e.paid, k)
			paneCalls++
			jobs = append(jobs, job{dot: d, turns: tail(turns, d.Context.Turns), forceQueue: anyFromDot(newTurns)})
		}
	})
	e.mu.Unlock()
	if turnEnded {
		e.limiter().endTurn(paneID) // the per-turn ask limit starts over
	}
	return jobs, budgetHit
}

// upgradeFromScrollback retries reader selection while the pane's cached reader
// is a scrollback *fallback* (the session is known and scrollback wasn't
// forced): Claude writes <session>.jsonl only on the first prompt, so Select
// falls back to scrollback right after attach or /clear. Runs with the pane
// lock held.
//
// Rule on upgrade: the transcript's existing turns count as already seen,
// except the last one, which this (idle-only) pass reviews. Turns before it
// were covered, lossily, by scrollback; the last one is the turn that just
// finished. At worst that one turn is reviewed twice (once per reader).
func (e *Engine) upgradeFromScrollback(att state.Attachment) state.Attachment {
	if att.ForceScrollback || att.SessionValue == "" || e.reader(att).Source() != transcript.SourceScrollback {
		return att
	}
	nr := e.d.Reader(att)
	if nr.Source() == transcript.SourceScrollback {
		return att
	}
	turns, err := nr.Turns(e.ctx)
	if err != nil {
		return att // not readable yet; try again on the next pass
	}
	seen := max(0, len(turns)-1)
	applied := false
	err = e.d.Attachments.Update(att.PaneID, func(a *state.Attachment) {
		if a.SessionValue != att.SessionValue {
			return
		}
		applied = true
		a.Source = nr.Source()
		for i := range a.Dots {
			a.Dots[i].LastSeenTurn = seen
			a.Dots[i].MemorySeenTurn = seen // scrollback turn numbers don't carry over
		}
	})
	if err != nil || !applied {
		return att
	}
	e.mu.Lock()
	e.readers[att.PaneID] = nr
	e.mu.Unlock()
	e.d.Logf("pane %s: transcript available; switched from scrollback to %s", att.PaneID, nr.Source())
	if a, ok := e.d.Attachments.Get(att.PaneID); ok {
		return a
	}
	return att
}

func (e *Engine) runDot(paneID string, d dots.Dot, turns []transcript.Turn, o runOpts) {
	k := key{paneID, d.Slug}
	base := e.ctx
	if o.ctx != nil {
		base = o.ctx
	}
	sinkRes, sinkErr := backend.Result{}, errRunNotStarted
	if o.sink != nil {
		defer func() { o.sink(sinkRes, sinkErr) }() // registered first: runs last
	}
	defer func() {
		if !o.summon {
			e.finishRun(k)
		} else {
			e.broadcast()
		}
	}()
	select {
	case e.sem <- struct{}{}:
	case <-base.Done():
		sinkErr = base.Err()
		return
	}
	defer func() { <-e.sem }()

	var prev string // the status before this run's "thinking", restored when an ask is cancelled
	if o.memoryPass {
		if !e.memOn(d) {
			return // memory was turned off after the pass was scheduled
		}
		e.setLearning(k, true)
		defer e.setLearning(k, false)
	} else {
		prev = e.statusOf(k)
		e.setStatus(k, view.StatusThinking)
	}
	att, _ := e.d.Attachments.Get(paneID)
	turn := 0
	if len(turns) > 0 {
		turn = turns[len(turns)-1].Index
	}
	be, err := e.d.Backend(d.Backend.Kind)
	if err != nil {
		err = redactErr(err)
		sinkErr = err
		e.runFailed(k, d, turn, err)
		return
	}
	mem := e.loadMemory(&att, d, turns) // outside every engine lock
	if o.noLearn {
		mem.learn = false // no remember ops asked for, none applied
	}
	req := requestFor(d, att.Cwd)
	req.Turns, req.Question, req.Force = turns, o.question, o.summon && !o.noForce
	req.Memory, req.Learn = mem.block, mem.learn
	if o.memoryPass {
		if mem.project == "" {
			return // no project: the pass's ops could not be stored, so skip the call
		}
		req.Pass, req.RepoRead, req.Question, req.Force = backend.PassMemory, false, "", false
	}
	ctx, cancel := context.WithTimeout(base, e.config().RunTimeout())
	defer cancel()
	res, err := be.Run(ctx, req)
	e.chargeRun(k, paneID, d.Slug, deltaOf(res), !o.summon)
	e.hook("run-charged")
	if err != nil {
		err = redactErr(err) // defense in depth: adapters redact already
		sinkErr = err
		if errors.Is(base.Err(), context.Canceled) {
			// Its council was abandoned (the pane closed) or the engine is
			// stopping: not the dot's failure, and maybe no pane to show it.
			// A real timeout is DeadlineExceeded and still fails below.
			e.d.Logf("dot %s on %s: run cancelled: %v", d.Slug, paneID, err)
			if o.ask || (o.sink != nil && !o.memoryPass) {
				e.restoreStatus(k, prev) // an ask or a council member: put the status back
			}
			return
		}
		e.runFailed(k, d, turn, err)
		return
	}
	sinkRes, sinkErr = res, nil
	// The agent gave up or the engine stopped (or, for a council member, its
	// council was abandoned): learn and log nothing. A council member that
	// merely hit the council's deadline still reports its answer.
	gaveUp := func() bool {
		return (o.ask && base.Err() != nil) || (!o.ask && o.sink != nil && !o.memoryPass && errors.Is(base.Err(), context.Canceled))
	}
	if gaveUp() {
		e.restoreStatus(k, prev)
		return
	}
	e.touchMemory(mem)
	_ = e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		if da := a.Find(d.Slug); da != nil {
			da.Failures = 0
			switch {
			case o.memoryPass:
				// A pass from an old agent session charges nothing to the new one.
				if a.SessionValue == att.SessionValue {
					da.MemoryPasses++
					if o.seenTo > da.MemorySeenTurn {
						da.MemorySeenTurn = o.seenTo
					}
				}
			case res.Verdict.Relevant && !o.ask:
				da.CooldownLeft = d.Trigger.Cooldown
			}
		}
	})
	if o.memoryPass {
		e.clearError(k) // a pass sets no status of its own; drop a stale failure
		if e.applyMemoryOps(paneID, d, turn, mem, res.Remember, att, true) == 0 {
			e.feed(paneID, d.Slug, turn, state.OutcomePass, "memory pass: nothing new")
		}
		return
	}
	// Checked again right before the memory ops: touching memory and the
	// attachment take time, and a cancel that lands meanwhile must still
	// keep the reply out of memory.
	if gaveUp() {
		e.restoreStatus(k, prev)
		return
	}
	e.applyMemoryOps(paneID, d, turn, mem, res.Remember, att, false)
	if o.ask {
		if base.Err() != nil {
			e.restoreStatus(k, prev)
			return
		}
		e.feed(paneID, d.Slug, turn, state.OutcomeAsked, askDetail(d, res))
		e.setStatus(k, view.StatusConsulted)
		return // MCP ask: the answer goes back to the agent, never into the queue
	}
	if res.Verdict.Relevant && !o.summon {
		// Anti-repeat uses the candidates loaded before the run, so the ops
		// just applied never suppress their own verdict.
		if ref, ok := suppressedBy(mem, res.Verdict.Message, turn, att.SessionValue); ok {
			e.feed(paneID, d.Slug, turn, state.OutcomeSuppressed, "similar to "+ref)
			e.setStatus(k, view.StatusPassed)
			return
		}
	}
	if !res.Verdict.Relevant {
		e.feed(paneID, d.Slug, turn, state.OutcomePass, costDetail(d, res))
		e.setStatus(k, view.StatusPassed)
		return
	}
	e.feed(paneID, d.Slug, turn, state.OutcomeFlagged, withCost(res.Verdict.Message, costDetail(d, res)))
	e.setStatus(k, view.StatusFlagged)
	if o.sink != nil {
		return // council member: the council turns every answer into one card
	}
	e.route(paneID, d, turn, res.Verdict, o)
}

func (e *Engine) runFailed(k key, d dots.Dot, turn int, err error) {
	err = redactErr(err) // defense in depth: adapters redact already
	// Run errors can carry provider text (a refusal): no control characters
	// in the log, the feed or a toast.
	msg := textutil.SanitizeModelText(err.Error())
	e.d.Logf("dot %s on %s: %s", d.Slug, k.pane, msg)
	e.feed(k.pane, d.Slug, turn, state.OutcomeError, msg)
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
		e.notify(d, fmt.Sprintf("stopped after %d failures: %s (press r in the Dots pane to reset)", e.d.MaxFailures, msg))
	}
}

func (e *Engine) finishRun(k key) {
	e.mu.Lock()
	delete(e.running, k)
	delete(e.paid, k)
	again := e.rerun[k]
	delete(e.rerun, k)
	e.mu.Unlock()
	e.broadcast()
	if again {
		e.spawn(func() { e.processPaneIfIdle(k.pane) })
	}
}

// route turns a relevant verdict into a card according to the dot's mode.
func (e *Engine) route(paneID string, d dots.Dot, turn int, v backend.Verdict, o runOpts) {
	c, mode := e.routeCard(paneID, d, turn, v, o)
	if _, err := e.d.Queue.Add(c); err != nil {
		e.d.Logf("queue add: %v", err)
		return
	}
	e.routed(paneID, d, v, mode)
}

// routeCard is the card route adds for a verdict, and its delivery mode.
func (e *Engine) routeCard(paneID string, d dots.Dot, turn int, v backend.Verdict, o runOpts) (queue.Card, dots.Mode) {
	mode := d.Delivery.Mode
	if o.summon || mode == dots.ModeObserve {
		mode = dots.ModeQueue
	}
	if mode == dots.ModeInject && (o.forceQueue || !e.config().InjectEnabled) {
		mode = dots.ModeQueue
	}
	att, _ := e.d.Attachments.Get(paneID)
	c := queue.Card{Dot: d.Slug, DotName: d.Name, Glyph: d.Glyph, PaneID: paneID, Turn: turn,
		Severity: v.Severity, Body: v.Message, Mode: string(mode), Project: att.ProjectID, Origin: o.origin, Label: o.label}
	if mode == dots.ModeInject {
		c.Status = queue.StatusAuto
	}
	return c, mode
}

// routed follows a card route added: the toast, the pending count, and the
// flush of an auto card.
func (e *Engine) routed(paneID string, d dots.Dot, v backend.Verdict, mode dots.Mode) {
	if mode == dots.ModeSuggest && (v.Severity == "warn" || v.Severity == "critical") {
		e.notify(d, firstLine(v.Message))
	}
	e.reportPending(paneID)
	e.broadcast()
	if mode == dots.ModeInject {
		e.flushAuto(paneID)
	}
}

func (e *Engine) statusOf(k key) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status[k]
}

// restoreStatus puts back the status a cancelled ask replaced, unless
// something else has set a new one meanwhile. A closed pane keeps no status:
// a reused pane id must not inherit it.
func (e *Engine) restoreStatus(k key, prev string) {
	e.mu.Lock()
	if _, ok := e.d.Attachments.Get(k.pane); !ok { // e.mu -> stores; dropPane deletes under e.mu
		delete(e.status, k)
	} else if e.status[k] == view.StatusThinking {
		e.status[k] = prev
	}
	e.mu.Unlock()
	e.broadcast()
}

// charge records a run's usage, unless its pane closed meanwhile: dropPane
// reset the pane's usage, and a reused pane id must not inherit the spend.
// The check and the add share e.mu with dropPane's reset and delete.
func (e *Engine) charge(paneID, slug string, d state.Delta) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.chargeLocked(paneID, slug, d)
}

// chargeRun is charge for a turn review or memory pass (k = pane + dot). When
// the run is tracked in e.running (review is true: not a manual summon), the
// same e.mu section marks it paid, so inFlightLocked stops counting it: its
// usage is in Usage now, and counting both would refuse a concurrent ask or
// background run at the budget edge for a run that already finished and paid.
// e.running stays set until finishRun, which keeps the one-run-per-dot and
// rerun rules.
func (e *Engine) chargeRun(k key, paneID, slug string, d state.Delta, review bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.chargeLocked(paneID, slug, d)
	if review && e.running[k] {
		e.paid[k] = true
	}
}

// chargeLocked is charge's body. Caller holds e.mu.
func (e *Engine) chargeLocked(paneID, slug string, d state.Delta) {
	if _, ok := e.d.Attachments.Get(paneID); !ok {
		return
	}
	_ = e.d.Usage.Add(paneID, slug, d)
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
	s := view.Snapshot{InjectEnabled: e.config().InjectEnabled, PausedAll: e.pausedAll, WriteEnabled: e.config().WriteEnabled}
	now := e.now()
	if day := e.todayLocked(now); e.d.Daily != nil {
		s.TodayUSD, s.TodayRuns, s.DailyBudgetUSD = day.CostUSD, day.Total(), dailyBudget(e.config().DailyBudgetUSD)
	}
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
			Backend: d.Backend.Kind, Model: d.Backend.Model, Mode: string(d.Delivery.Mode),
			Write: d.Capabilities.Write && dots.CanWrite(d.Backend.Kind), WriteTimeoutSeconds: d.Capabilities.WriteTimeoutSeconds})
	}
	for _, le := range e.invalid {
		s.Invalid = append(s.Invalid, view.Invalid{File: le.File, Line: le.Line, Error: le.Err.Error()})
	}
	seen := map[string]bool{}
	for _, a := range e.d.Attachments.List() {
		ms := e.memStats[a.ProjectID]
		pv := view.Pane{PaneID: a.PaneID, Agent: a.Agent, Source: a.Source,
			Calls: e.d.Usage.PaneCalls(a.PaneID), Budget: e.config().SessionBudgetCalls,
			CostUSD: e.d.Usage.PaneCost(a.PaneID), CostUnknown: !e.d.Usage.PaneCostKnown(a.PaneID),
			BudgetUSD: e.config().SessionBudgetUSD,
			ProjectID: a.ProjectID, ProjectName: a.ProjectName, MemoryEntries: ms.Total(), MemoryProposals: ms.Proposals}
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
				Calls: e.d.Usage.Calls(a.PaneID, da.Slug), Budget: d.Budget.MaxCallsPerSession,
				MemoryEntries: ms.Entries[da.Slug], Learning: e.learning[key{a.PaneID, da.Slug}],
				CostUSD: e.d.Usage.Cost(a.PaneID, da.Slug), CostUnknown: !e.d.Usage.CostKnown(a.PaneID, da.Slug),
				BudgetUSD: d.Budget.MaxUSDPerSession})
		}
		for i := range pv.Dots { // Phase 8: proactive hint and background run per dot
			if d, ok := e.dots[pv.Dots[i].Slug]; ok {
				pv.Dots[i].Next = e.nextLocked(d, a.PaneID, now)
			}
			pv.Dots[i].Background = e.bgBusy[key{a.PaneID, pv.Dots[i].Slug}]
		}
		pv.Away = e.awayLocked(a.PaneID)
		pv.Cards = e.d.Queue.Visible(a.PaneID)
		pv.Drafts = e.draftViews(a.PaneID)
		pv.Feed = e.d.Feed.Recent(a.PaneID, 20)
		pv.Council, pv.CouncilPreflight = e.councilViewLocked(a.PaneID)
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
