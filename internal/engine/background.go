package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/transcript"
)

// Proactive work (Phase 8): idle research (idle.go), scheduled dots
// (schedule.go) and commit triggers (commits.go) all go through
// runBackground. Background runs are read-only, one at a time per engine,
// capped per dot per day and by the daily dollar budget shared by every
// Herdr session (state.Daily), and never touch the disk under an engine lock.

// Reasons a background run does not start (besides the v1 ones).
const (
	ReasonProactiveOff     Reason = "proactive_off"     // proactive_enabled = false, or no daily store
	ReasonDailyRuns        Reason = "daily_runs"        // the dot's runs of this kind today
	ReasonDailyBudget      Reason = "daily_budget"      // daily_budget_usd spent today, every session
	ReasonDailyUnavailable Reason = "daily_unavailable" // daily.json locked, unreadable or blocked: fail closed
)

// How often poll re-reads daily.json for the snapshot, and sweeps the inbox
// for attached projects (a result from another session lands within this).
const (
	todayEvery = 30 * time.Second
	inboxEvery = 30 * time.Second
)

// BackgroundInput is what DecideBackground looks at.
type BackgroundInput struct {
	Proactive   bool    // config proactive_enabled and a daily store exists
	PausedAll   bool    // the Dots pane's pause-all
	Paused      bool    // the dot is paused on the pane the run belongs to
	DailyCount  int     // this dot's runs of this kind today, every session
	MaxPerDay   int     // per-dot cap for this kind; 0 = none
	CostToday   float64 // every background run today, every session
	DailyBudget float64 // config daily_budget_usd (always > 0)
}

// dailyBudget is the budget DecideBackground applies: never "unlimited". A
// zero, negative, NaN or infinite figure (config.Load never yields one, but
// Deps.Config is not validated) falls back to the default.
func dailyBudget(b float64) float64 {
	if !(b > 0) || math.IsInf(b, 0) {
		return config.DefaultDailyBudgetUSD
	}
	return b
}

// DecideBackground applies the caps every background run shares.
func DecideBackground(in BackgroundInput) Reason {
	switch {
	case !in.Proactive:
		return ReasonProactiveOff
	case in.PausedAll:
		return ReasonPausedAll
	case in.Paused:
		return ReasonPaused
	case in.MaxPerDay > 0 && in.DailyCount >= in.MaxPerDay:
		return ReasonDailyRuns
	case OverUSD(in.CostToday, dailyBudget(in.DailyBudget)):
		return ReasonDailyBudget
	}
	return ReasonRun
}

// bgJob is one background run.
type bgJob struct {
	kind        string // state.RunResearch, RunScheduled or RunCommit
	paneID      string // "" = no pane attached on the project (scheduled runs only)
	session     string // the pane's agent session when the job was built
	project     string // memory project id; "" = no memory for this run
	projectName string
	cwd         string // repo the run may read; "" = no repo tools
	dot         dots.Dot
	turns       []transcript.Turn
	question    string
	label       string    // feed and card header: "research", "scheduled Mon 09:00", "commit a1b2c3d"
	maxPerDay   int       // per-dot daily cap for this kind; 0 = none
	paused      bool      // the dot is paused on paneID
	gen         uint64    // paneID's attachment generation (e.gens) when the job was built
	turn        int       // the pane's last turn index, for memory evidence (0 without a pane)
	period      int       // research: the pane's idle period when the run was reserved
	since       time.Time // research: when that idle period began
	deliver     func(j bgJob, v backend.Verdict)
	done        func()      // called exactly once when the job ends, run or not
	claim       func() bool // scheduled: claims the slot once every gate passed; false = not this engine's to run
}

// now is the engine's clock (Deps.Clock): every proactive timer reads it.
func (e *Engine) now() time.Time {
	if e.d.Clock != nil {
		return e.d.Clock()
	}
	return time.Now()
}

// proactiveOn: a daily store exists and proactive_enabled is true. config.toml
// reloads at runtime, so this is read at the point of use.
func (e *Engine) proactiveOn() bool { return e.d.Daily != nil && e.config().ProactiveEnabled }

func dayOf(t time.Time) string { return t.Format("2006-01-02") }

// todayLocked is the cached day if it is still today. Called with e.mu held.
func (e *Engine) todayLocked(now time.Time) state.Day {
	if e.today.Date != dayOf(now) {
		return state.Day{Date: dayOf(now)}
	}
	return e.today
}

// dailyClosedLocked: today's background work is stopped because daily.json
// could not be trusted (a Blocked day, or a run whose cost could not be
// recorded). Callers treat it as "budget reached". Called with e.mu held.
func (e *Engine) dailyClosedLocked(now time.Time) bool {
	return e.todayLocked(now).Blocked || e.dailyErr == dayOf(now)
}

func (e *Engine) setToday(day state.Day) {
	e.mu.Lock()
	e.today, e.todayAt = day, e.now()
	e.mu.Unlock()
}

// refreshToday re-reads daily.json for the snapshot. No engine lock held.
func (e *Engine) refreshToday() {
	if e.d.Daily == nil {
		return
	}
	day, err := e.d.Daily.Today(e.now())
	if err != nil {
		e.d.Logf("daily budget: %v", err)
		return
	}
	e.setToday(day)
	if e.proactiveOn() && OverUSD(day.CostUSD, dailyBudget(e.config().DailyBudgetUSD)) {
		e.dailyBudgetHit() // one toast a day, even when no run reaches the gate
	}
	e.broadcast()
}

// todayTick refreshes the cached day every todayEvery. Called from poll.
func (e *Engine) todayTick() {
	e.mu.Lock()
	due := e.now().Sub(e.todayAt) >= todayEvery
	e.mu.Unlock()
	if due {
		e.refreshToday()
	}
}

// backgroundGate reads daily.json and applies DecideBackground. No engine
// lock may be held: it does file I/O. Any error reading the day, a Blocked
// day, or an earlier failure to record a run's cost today stops the run
// (state.Daily's contract: these mean "budget reached", never unlimited).
func (e *Engine) backgroundGate(slug, kind string, maxPerDay int, paused bool) Reason {
	cfg := e.config()
	in := BackgroundInput{Proactive: e.proactiveOn(), Paused: paused, MaxPerDay: maxPerDay, DailyBudget: cfg.DailyBudgetUSD}
	e.mu.Lock()
	in.PausedAll = e.pausedAll
	e.mu.Unlock()
	if in.Proactive {
		now := e.now()
		day, err := e.d.Daily.Today(now)
		if err != nil {
			e.d.Logf("daily budget: %v (background work waits)", err)
			return ReasonDailyUnavailable
		}
		e.setToday(day)
		e.mu.Lock()
		closed := e.dailyClosedLocked(now)
		e.mu.Unlock()
		if closed {
			if e.onceToday("daily-closed") {
				e.d.Logf("daily budget: daily.json could not be trusted today; background work is paused until midnight")
			}
			e.broadcast()
			return ReasonDailyUnavailable
		}
		in.DailyCount, in.CostToday = day.Count(slug, kind), day.CostUSD
	}
	r := DecideBackground(in)
	if r == ReasonDailyBudget {
		e.dailyBudgetHit()
	}
	return r
}

// dailyBudgetHit toasts once per day that background work has stopped.
func (e *Engine) dailyBudgetHit() {
	msg := fmt.Sprintf("daily budget $%.2f reached: research, schedules and commit reviews are paused until midnight", dailyBudget(e.config().DailyBudgetUSD))
	if e.onceToday("daily-budget") {
		e.d.Logf("%s", msg)
		if err := e.d.Herdr.Notify(e.ctx, "Herdr Dots", msg); err != nil {
			e.d.Logf("notify: %v", err)
		}
	}
	e.broadcast()
}

// onceToday reports true the first time it is called with key on a day.
func (e *Engine) onceToday(key string) bool {
	day := dayOf(e.now())
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.toasted[key] == day {
		return false
	}
	e.toasted[key] = day
	return true
}

func outcomeFor(kind string) string {
	switch kind {
	case state.RunResearch:
		return state.OutcomeResearch
	case state.RunScheduled:
		return state.OutcomeScheduled
	}
	return state.OutcomeCommit
}

// runBackground runs one job: one background run at a time (bgSem), then a
// run slot (sem), a fresh daily check, the pane checks (still attached, same
// generation, session budgets), the backend, usage and daily accounting,
// memory ops, anti-repeat, then j.deliver for a relevant verdict. It never
// asks for write tools: the request is a review or a research pass.
//
// A job is queued (bgQueued, for dedupe) from entry to exit, and running
// (bgBusy: in flight for the budgets, and the roster's label) only while it
// holds bgSem. With one running job per engine, bgBusy has at most one entry,
// always the running job's.
func (e *Engine) runBackground(j bgJob) {
	k := key{j.paneID, j.dot.Slug}
	e.mu.Lock()
	e.bgQueued[k]++
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		if e.bgQueued[k]--; e.bgQueued[k] <= 0 {
			delete(e.bgQueued, k)
		}
		e.mu.Unlock()
		if j.done != nil {
			j.done()
		}
		e.broadcast()
	}()
	select {
	case e.bgSem <- struct{}{}:
	case <-e.ctx.Done():
		return
	}
	e.mu.Lock()
	e.bgBusy[k] = j.kind
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.bgBusy, k) // before the slot is released: the next job may share the key
		e.mu.Unlock()
		<-e.bgSem
	}()
	e.broadcast()
	select {
	case e.sem <- struct{}{}:
	case <-e.ctx.Done():
		return
	}
	defer func() { <-e.sem }()

	// Checked again after the wait: another dot or session may have spent the budget.
	if r := e.backgroundGate(j.dot.Slug, j.kind, j.maxPerDay, j.paused); r != ReasonRun {
		e.d.Logf("dot %s: %s skipped (%s)", j.dot.Slug, j.label, r)
		return
	}
	if !e.keepPane(&j, "started") {
		return
	}
	e.mu.Lock()
	over := e.bgOverBudgetLocked(j)
	e.mu.Unlock()
	if over {
		e.d.Logf("dot %s: %s skipped (%s on %s)", j.dot.Slug, j.label, ReasonSessionBudget, j.paneID)
		return
	}
	// A scheduled slot is claimed (across sessions) only now, after every gate:
	// a slot skipped for a budget stays unclaimed, so a later catch-up may run it.
	if j.claim != nil && !j.claim() {
		return
	}
	be, err := e.d.Backend(j.dot.Backend.Kind)
	if err != nil {
		e.bgFailed(j, err)
		return
	}
	att := state.Attachment{PaneID: j.paneID, Cwd: j.cwd, ProjectID: j.project, ProjectName: j.projectName, SessionValue: j.session}
	mem := e.loadMemory(&att, j.dot, j.turns) // memory goes in the user prompt only
	req := requestFor(j.dot, j.cwd)
	req.Turns, req.Question, req.Memory, req.Learn = j.turns, j.question, mem.block, mem.learn
	req.RepoRead = j.cwd != "" && (j.kind != state.RunCommit || j.dot.Context.RepoRead)
	switch j.kind {
	case state.RunResearch:
		req.Pass = backend.PassResearch // read-only, like a review (draft_astguard_test.go allows exactly this)
		req.Web = j.dot.Research.Web && backend.WebSupported(j.dot.Backend.Kind)
		if j.dot.Research.Web && !req.Web {
			e.d.Logf("dot %s: %s has no web tools; researching without the web", j.dot.Slug, j.dot.Backend.Kind)
		}
	case state.RunScheduled:
		req.Trigger = backend.TriggerScheduled
	case state.RunCommit:
		req.Trigger = backend.TriggerCommit
	}
	if c := e.bgRunCap(); c > 0 { // the gate stopped the run if nothing is left
		req.MaxBudgetUSD = c
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.config().RunTimeout())
	defer cancel()
	res, err := be.Run(ctx, req)
	e.chargeBackground(j, deltaOf(res)) // not to a pane that closed (or whose id was reused) meanwhile
	cost := 0.0                         // unknown-cost runs count one run and $0 (Phase 4 rule)
	if res.CostKnown {
		cost = res.CostUSD
	}
	if derr := e.d.Daily.Add(j.dot.Slug, j.kind, cost, e.now()); derr != nil {
		// The spend is unrecorded: count today as "budget reached" in this
		// engine (it resumes at local midnight), never as unlimited.
		e.mu.Lock()
		e.dailyErr = dayOf(e.now())
		e.mu.Unlock()
		e.d.Logf("daily budget: %v (background work paused until midnight)", derr)
	}
	e.refreshToday()
	if !e.keepPane(&j, "ended") {
		return
	}
	if err != nil {
		e.bgFailed(j, err)
		return
	}
	e.touchMemory(mem)
	e.applyMemoryOps(j.paneID, j.dot, j.turn, mem, res.Remember, att, false, memory.ApplyOptions{Research: j.kind == state.RunResearch})
	e.hook("bg-before-feed")
	// The memory I/O took time: the feed line re-checks the pane (bgFeed).
	v, detail := res.Verdict, costDetail(j.dot, res)
	if !v.Relevant {
		e.bgFeed(&j, outcomeFor(j.kind), withCost(j.label+": nothing new", detail))
		return
	}
	if ref, ok := suppressedBy(mem, v.Message, j.turn, j.session); ok {
		e.bgFeed(&j, state.OutcomeSuppressed, "similar to "+ref)
		return
	}
	if !e.bgFeed(&j, outcomeFor(j.kind), withCost(j.label+": "+firstLine(v.Message), detail)) {
		return
	}
	if j.deliver != nil {
		j.deliver(j, v)
	}
}

// bgRunMaxUSD is the most one background run may spend where the backend
// can stop it (claude-cli --max-budget-usd; ruling P8-R15).
const bgRunMaxUSD = 0.50

// bgRunCap is a background run's dollar cap: the daily budget left (the
// day backgroundGate just read), at most bgRunMaxUSD, rounded to a
// millionth of a dollar. It may be zero or less; the caller passes only a
// positive cap.
func (e *Engine) bgRunCap() float64 {
	budget := dailyBudget(e.config().DailyBudgetUSD)
	now := e.now()
	e.mu.Lock()
	spent := e.todayLocked(now).CostUSD
	e.mu.Unlock()
	return math.Round(min(budget-spent, bgRunMaxUSD)*1e6) / 1e6
}

// bgFeed writes a job's feed line with the pane check in the same e.mu
// section (e.mu -> stores), so a pane closed or reused meanwhile gets no
// line. As in keepPane, a scheduled job whose pane is gone carries on
// without it (the line goes to no pane, the result to the inbox); any other
// job ends. It reports whether the job continues.
func (e *Engine) bgFeed(j *bgJob, outcome, detail string) bool {
	e.mu.Lock()
	if e.bgPaneGoneLocked(*j) {
		if j.kind != state.RunScheduled {
			e.mu.Unlock()
			e.d.Logf("dot %s: %s ended after pane %s closed; dropped", j.dot.Slug, j.label, j.paneID)
			return false
		}
		j.paneID, j.session, j.turn, j.paused = "", "", 0, false
	}
	err := e.d.Feed.Append(state.FeedEntry{Time: time.Now(), PaneID: j.paneID, Dot: j.dot.Slug, Turn: j.turn, Outcome: outcome, Detail: detail})
	e.mu.Unlock()
	if err != nil {
		e.d.Logf("feed: %v", err)
	}
	return true
}

// bgPaneGoneLocked reports whether a job's pane closed, or its id now names
// a new attachment (the generation changed, as for Phase 7 drafts), since the
// job was built. Caller holds e.mu (e.mu -> stores).
func (e *Engine) bgPaneGoneLocked(j bgJob) bool {
	if j.paneID == "" {
		return false
	}
	_, ok := e.d.Attachments.Get(j.paneID)
	return !ok || e.gens[j.paneID] != j.gen
}

func (e *Engine) bgPaneGone(j bgJob) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.bgPaneGoneLocked(j)
}

// keepPane decides what a job does when its pane is gone: a scheduled run
// belongs to its project, so it carries on without the pane (its result goes
// to the project's inbox); research and commit reviews end, with no card,
// no feed entry and no charge. It reports whether the job continues.
func (e *Engine) keepPane(j *bgJob, when string) bool {
	if !e.bgPaneGone(*j) {
		return true
	}
	if j.kind == state.RunScheduled {
		j.paneID, j.session, j.turn, j.paused = "", "", 0, false
		return true
	}
	e.d.Logf("dot %s: %s %s after pane %s closed; dropped", j.dot.Slug, j.label, when, j.paneID)
	return false
}

// chargeBackground adds a run's usage to its pane's session usage, unless
// the pane is gone (dropPane reset its usage; a reused id must not inherit
// the spend). The check and the add share e.mu, as in charge and in Phase 7's
// charge for a draft.
func (e *Engine) chargeBackground(j bgJob, d state.Delta) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if j.paneID == "" || e.bgPaneGoneLocked(j) {
		return
	}
	_ = e.d.Usage.Add(j.paneID, j.dot.Slug, d)
}

// inFlightLocked counts the pane's runs that may still charge it: turn
// reviews and memory passes (e.running), MCP asks, live drafts and the
// running background job (bgBusy; a job still waiting for bgSem has not
// started and re-checks the budgets itself), except the background run keyed
// except. Every session-budget check (turn reviews, asks, drafts, research,
// commit and background runs) counts them as calls already made (rulings
// P8-R2 and P8-R8). Caller holds e.mu.
func (e *Engine) inFlightLocked(paneID string, except key) int {
	n := 0
	for k := range e.running {
		if k.pane == paneID {
			n++
		}
	}
	for k := range e.asking {
		if k.pane == paneID {
			n++
		}
	}
	for _, dr := range e.drafts {
		if dr.pane == paneID && !dr.abandoned {
			n++
		}
	}
	for k := range e.bgBusy {
		if k.pane == paneID && k != except {
			n++
		}
	}
	return n
}

// bgOverBudgetLocked: a background run on a pane counts as one call (and its
// cost) toward the dot's and the pane's session budgets, like a draft
// (draftBudgetLocked): asks, drafts and other background runs in flight count
// as calls already made. A pane-less scheduled run has no session budget.
// Caller holds e.mu.
func (e *Engine) bgOverBudgetLocked(j bgJob) bool {
	if j.paneID == "" {
		return false
	}
	cfg := e.config()
	inFlight := e.inFlightLocked(j.paneID, key{j.paneID, j.dot.Slug})
	return e.d.Usage.Calls(j.paneID, j.dot.Slug) >= j.dot.Budget.MaxCallsPerSession ||
		OverUSD(e.d.Usage.Cost(j.paneID, j.dot.Slug), j.dot.Budget.MaxUSDPerSession) ||
		e.d.Usage.PaneCalls(j.paneID)+inFlight >= cfg.SessionBudgetCalls ||
		OverUSD(e.d.Usage.PaneCost(j.paneID), cfg.SessionBudgetUSD)
}

// bgFailed logs and feeds a failed run (redacted and sanitized, like
// runFailed). Background failures never count toward the 3-failure stop.
func (e *Engine) bgFailed(j bgJob, err error) {
	msg := textutil.SanitizeModelText(redactErr(err).Error())
	e.d.Logf("dot %s: %s failed: %s", j.dot.Slug, j.label, msg)
	e.bgFeed(&j, state.OutcomeError, j.label+": "+msg)
}

// deliverScheduled puts a scheduled result on the pane the job ran for, or,
// with no pane on the project, in the shared inbox with a toast. It is a
// queue card (suggestions stay suggestions): never auto-delivered. The pane
// check and the add share e.mu (e.mu -> stores), as Phase 7 drafts do, so a
// pane closing (or its id being reused) meanwhile never gets the card.
func (e *Engine) deliverScheduled(j bgJob, v backend.Verdict) {
	mode := dots.ModeQueue
	if j.dot.Delivery.Mode == dots.ModeSuggest {
		mode = dots.ModeSuggest
	}
	c := queue.Card{Dot: j.dot.Slug, DotName: j.dot.Name, Glyph: j.dot.Glyph, Turn: j.turn, Severity: v.Severity,
		Body: v.Message, Mode: string(mode), Project: j.project, Origin: j.kind, Label: j.label}
	if j.paneID != "" {
		e.mu.Lock()
		added, gone := false, e.bgPaneGoneLocked(j)
		var err error
		if !gone {
			pc := c
			pc.PaneID = j.paneID
			_, err = e.d.Queue.Add(pc)
			added = err == nil
		}
		e.mu.Unlock()
		if err != nil {
			e.d.Logf("queue add: %v", err)
			return
		}
		if added {
			if mode == dots.ModeSuggest && (v.Severity == "warn" || v.Severity == "critical") {
				e.notify(j.dot, firstLine(v.Message))
			}
			e.reportPending(j.paneID)
			e.broadcast()
			return
		}
	}
	if e.d.Inbox == nil || j.project == "" {
		e.d.Logf("dot %s: %s result dropped: no pane on the project and no inbox", j.dot.Slug, j.label)
		return
	}
	if err := e.d.Inbox.Put(c); err != nil {
		e.d.Logf("inbox: %v", err)
		return
	}
	name := j.projectName
	if name == "" {
		name = j.project
	}
	e.notify(j.dot, fmt.Sprintf("%s (scheduled) has a finding for %s", j.dot.Name, name))
}

// claimInbox moves a project's waiting inbox cards into paneID's queue. The
// claim (inbox I/O) runs unlocked, so the pane is checked under e.mu before
// and after it: if it closed, was reused (its generation changed) or moved to
// another project meanwhile, the cards this claim added are orphaned, never
// left pending on a dead or reused pane id. dropPane orphans in its own e.mu
// section, so a pane closing after the re-check orphans them there.
func (e *Engine) claimInbox(paneID, projectID string) {
	if e.d.Inbox == nil || projectID == "" {
		return
	}
	e.mu.Lock()
	gen := e.gens[paneID]
	a, ok := e.d.Attachments.Get(paneID)
	e.mu.Unlock()
	if !ok || a.ProjectID != projectID {
		return
	}
	before := map[string]bool{} // cards already claimed onto the pane
	for _, c := range e.d.Queue.All() {
		if c.PaneID == paneID && c.SourceID != "" {
			before[c.ID] = true
		}
	}
	e.hook("inbox-before-claim")
	n, err := e.d.Queue.Claim(e.d.Inbox, projectID, paneID)
	if err != nil {
		e.d.Logf("inbox claim for %s: %v", paneID, err)
	}
	if n == 0 {
		return
	}
	stale := 0
	e.mu.Lock()
	if cur, ok := e.d.Attachments.Get(paneID); !ok || cur.ProjectID != projectID || e.gens[paneID] != gen {
		for _, c := range e.d.Queue.All() {
			if c.PaneID == paneID && c.SourceID != "" && !before[c.ID] && c.Status == queue.StatusPending {
				if e.d.Queue.SetStatus(c.ID, queue.StatusOrphaned) == nil {
					stale++
				}
			}
		}
	}
	e.mu.Unlock()
	if stale > 0 {
		e.d.Logf("pane %s closed during an inbox claim: %d card(s) orphaned", paneID, stale)
		e.broadcast()
		return
	}
	if n > 0 {
		e.d.Logf("pane %s: %d inbox card(s) for project %s", paneID, n, projectID)
		e.reportPending(paneID)
		e.broadcast()
	}
}

// inboxTick claims inbox cards for every attached project every inboxEvery,
// so a result another session filed reaches a pane here. Called from poll.
func (e *Engine) inboxTick() {
	if e.d.Inbox == nil {
		return
	}
	now := e.now()
	e.mu.Lock()
	due := now.Sub(e.inboxAt) >= inboxEvery
	if due {
		e.inboxAt = now
	}
	e.mu.Unlock()
	if !due {
		return
	}
	seen := map[string]bool{}
	for _, a := range e.d.Attachments.List() {
		if a.ProjectID != "" && !seen[a.ProjectID] {
			seen[a.ProjectID] = true
			e.claimInbox(a.PaneID, a.ProjectID)
		}
	}
}

// nextLocked is the roster's proactive hint for a dot on a pane. Called with
// e.mu held: no I/O.
func (e *Engine) nextLocked(d dots.Dot, paneID string, now time.Time) string {
	if !d.Proactive() || !e.proactiveOn() {
		return ""
	}
	if e.dailyClosedLocked(now) || OverUSD(e.todayLocked(now).CostUSD, dailyBudget(e.config().DailyBudgetUSD)) {
		return "paused · daily budget"
	}
	if s := e.researchNextLocked(d, paneID, now); s != "" {
		return s
	}
	return e.scheduleNextLocked(d, now)
}
