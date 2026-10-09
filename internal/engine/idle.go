package engine

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// Idle research (Phase 8, spec §6.1). A pane's idle period starts at its last
// activity: a new turn, a new agent session, the agent working, or the pane
// gaining focus (Herdr has no input-activity signal; Phase 8 V1). Each dot
// with [research] enabled runs at most one research pass per idle period,
// once the pane has been idle for the dot's idle_minutes. Relevant results
// collect per pane and become one "while you were away" digest card when the
// developer is back, or at once when a result is critical.

// The digest card's identity. "away" is not a dot: outcomes are recorded per
// part, like a council card.
const (
	digestSlug  = "away"
	digestName  = "Away"
	digestGlyph = "☾"
)

// Research request shape (spec R2).
const (
	researchTurns     = 10 // recent turns in a research request
	researchQuestions = 5  // newest open questions in the brief
	researchPaths     = 30 // touched files listed in the brief
)

// ResearchInput is what DecideResearch looks at for one dot on one pane.
type ResearchInput struct {
	Dot           dots.Dot
	Att           state.DotAttachment
	AgentIdle     bool          // Herdr reports the agent idle or done
	IdleFor       time.Duration // since the pane's idle period began
	RanThisPeriod bool          // this dot already researched in this idle period
	Busy          bool          // a background run for this dot on this pane is queued, reserved or running
	DotCalls      int
	PaneCalls     int
	PaneBudget    int
	DotCostUSD    float64
	PaneCostUSD   float64
	DotBudgetUSD  float64
	PaneBudgetUSD float64
	Background    BackgroundInput // the daily caps; MaxPerDay comes from the dot
}

// DecideResearch reports whether a dot should start a research pass now
// (spec §6.1 step 2): research on, agent idle long enough, not yet run this
// period, call and dollar budgets left, and the shared daily caps.
func DecideResearch(in ResearchInput) bool {
	r := in.Dot.Research
	switch {
	case !r.Enabled, !in.AgentIdle, in.Busy, in.RanThisPeriod:
		return false
	case in.IdleFor < time.Duration(r.IdleMinutes)*time.Minute:
		return false
	case in.DotCalls >= in.Dot.Budget.MaxCallsPerSession, in.PaneCalls >= in.PaneBudget:
		return false
	case OverUSD(in.DotCostUSD, in.DotBudgetUSD), OverUSD(in.PaneCostUSD, in.PaneBudgetUSD):
		return false
	}
	bg := in.Background
	bg.Paused = bg.Paused || in.Att.Paused
	bg.MaxPerDay = r.MaxRunsPerDay
	return DecideBackground(bg) == ReasonRun
}

// idleState is one pane's idle bookkeeping, guarded by e.mu. dropPane and the
// last Detach delete it in the section that bumps the pane's generation, so a
// reused pane id never inherits collected research.
type idleState struct {
	activeAt    time.Time      // the current idle period began here
	period      int            // starts at 1 (0 means "never ran" in ran); activity starts a new one
	turns       int            // highest LastSeenTurn seen
	session     string         // agent session seen
	focused     bool           // last PaneInfo.Focused
	agentIdle   bool           // last agent status was idle or done
	ran         map[string]int // dot slug -> period it last researched in
	inflight    int            // research runs reserved or running for this pane
	away        []awayPart     // relevant results not yet carded
	awaySince   time.Time      // the oldest collected part's idle period start
	flushWanted bool           // the developer came back while research ran
}

type awayPart struct {
	slug, name, glyph string
	v                 backend.Verdict
}

// observePane updates a pane's idle period from a fresh PaneInfo, flushes
// the digest when the developer is back, and starts research that is due.
// Called from poll, with no engine lock held.
func (e *Engine) observePane(a state.Attachment, p herdr.PaneInfo) {
	now := e.now()
	agentIdle := p.AgentStatus == herdr.StatusIdle || p.AgentStatus == herdr.StatusDone
	seen := 0
	for _, da := range a.Dots {
		seen = max(seen, da.LastSeenTurn)
	}
	e.mu.Lock()
	if _, ok := e.d.Attachments.Get(a.PaneID); !ok {
		e.mu.Unlock() // dropped since poll listed it: no idle state for a dead pane
		return
	}
	st, ok := e.idle[a.PaneID]
	if !ok {
		st = &idleState{activeAt: now, period: 1, turns: seen, session: a.SessionValue, focused: p.Focused, ran: map[string]int{}}
		e.idle[a.PaneID] = st
	}
	active := !agentIdle || (p.Focused && !st.focused) || seen != st.turns || a.SessionValue != st.session
	st.focused, st.turns, st.session, st.agentIdle = p.Focused, seen, a.SessionValue, agentIdle
	flush := false
	if active {
		st.activeAt = now
		st.period++
		if len(st.away) > 0 {
			if st.inflight > 0 {
				st.flushWanted = true // one digest: wait for the runs still going
			} else {
				flush = true
			}
		}
	}
	e.mu.Unlock()
	if flush {
		e.flushAway(a.PaneID)
	}
	if agentIdle && e.proactiveOn() {
		e.maybeResearch(a, now)
	}
}

// maybeResearch reserves and starts every research pass that is due on a
// pane. The dedupe check and the reservation (bgQueued) share one e.mu
// section, so two polls can't both start a dot's pass; researchDone (j.done,
// called on every path out of the job) releases it.
func (e *Engine) maybeResearch(a state.Attachment, now time.Time) {
	cfg := e.config()
	var jobs []bgJob
	e.mu.Lock()
	st := e.idle[a.PaneID]
	if st == nil {
		e.mu.Unlock()
		return
	}
	day := e.todayLocked(now)
	// Asks, drafts and background runs in flight count as calls already made
	// (ruling P8-R8, the shared inFlightLocked).
	paneCalls := e.d.Usage.PaneCalls(a.PaneID) + e.inFlightLocked(a.PaneID, key{})
	paneCost := e.d.Usage.PaneCost(a.PaneID)
	gen := e.gens[a.PaneID] // the attachment these runs are for
	for _, da := range a.Dots {
		d, ok := e.dots[da.Slug]
		if !ok || !d.Research.Enabled {
			continue
		}
		k := key{a.PaneID, da.Slug}
		in := ResearchInput{Dot: d, Att: da, AgentIdle: st.agentIdle, IdleFor: now.Sub(st.activeAt),
			RanThisPeriod: st.ran[da.Slug] == st.period, Busy: e.bgQueued[k] > 0,
			DotCalls: e.d.Usage.Calls(a.PaneID, da.Slug), PaneCalls: paneCalls, PaneBudget: cfg.SessionBudgetCalls,
			DotCostUSD: e.d.Usage.Cost(a.PaneID, da.Slug), PaneCostUSD: paneCost,
			DotBudgetUSD: d.Budget.MaxUSDPerSession, PaneBudgetUSD: cfg.SessionBudgetUSD,
			Background: BackgroundInput{Proactive: true, PausedAll: e.pausedAll, DailyCount: day.Count(da.Slug, state.RunResearch),
				CostToday: day.CostUSD, DailyBudget: cfg.DailyBudgetUSD}}
		if !DecideResearch(in) || e.dailyClosedLocked(now) {
			continue
		}
		st.ran[da.Slug] = st.period
		st.inflight++
		e.bgQueued[k]++ // reserved: the next poll can't start it twice (released in researchDone)
		paneCalls++
		jobs = append(jobs, bgJob{kind: state.RunResearch, paneID: a.PaneID, session: a.SessionValue,
			project: a.ProjectID, projectName: a.ProjectName, cwd: a.Cwd, dot: d, label: "research",
			maxPerDay: d.Research.MaxRunsPerDay, paused: da.Paused, period: st.period, since: st.activeAt, gen: gen})
	}
	e.mu.Unlock()
	if len(jobs) > 0 {
		e.broadcast()
	}
	for _, j := range jobs {
		e.spawn(func() { e.runResearch(j) })
	}
}

// runResearch reads the turns and open questions (outside every lock) and
// runs the pass. Results go to the pane's away collector (addAway).
func (e *Engine) runResearch(j bgJob) {
	j.deliver = e.addAway
	j.done = func() { e.researchDone(j) }
	e.mu.Lock()
	att, ok := e.d.Attachments.Get(j.paneID)
	gone := e.bgPaneGoneLocked(j)
	e.mu.Unlock()
	if !ok || gone {
		j.done() // releases the reservation: runBackground never ran
		return
	}
	turns, err := e.reader(att).Turns(e.ctx)
	if err != nil {
		e.d.Logf("pane %s: research transcript read: %v", j.paneID, err)
	}
	j.turns = tail(turns, researchTurns)
	if len(j.turns) > 0 {
		j.turn = j.turns[len(j.turns)-1].Index
	}
	if j.project == "" {
		j.project = e.projectOf(&att)
	}
	j.question = e.researchBrief(j.project, j.dot, j.turns)
	e.runBackground(j)
}

// researchBrief is the research Question (spec R2): the dot's newest open
// questions from memory, its standing topics, and the recently touched files.
// It goes in the user prompt, like every memory text.
func (e *Engine) researchBrief(project string, d dots.Dot, turns []transcript.Turn) string {
	var b strings.Builder
	if qs := e.openQuestions(project, d); len(qs) > 0 {
		b.WriteString("Open questions you noted earlier (newest first):\n")
		for _, q := range qs {
			fmt.Fprintf(&b, "- [%s] %s\n", q.Ref(), strings.Join(strings.Fields(q.Text), " "))
		}
	}
	if topics := d.Research.PromptTopics(); len(topics) > 0 { // sanitized and capped, never the raw Topics
		b.WriteString("Topics you watch:\n")
		for _, t := range topics {
			fmt.Fprintf(&b, "- %s\n", strings.Join(strings.Fields(t), " "))
		}
	}
	var paths []string
	for _, p := range touchedPaths(turns) {
		if !slices.Contains(paths, p) && len(paths) < researchPaths {
			paths = append(paths, p)
		}
	}
	if len(paths) > 0 {
		b.WriteString("Files touched in the recent turns:\n")
		for _, p := range paths {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	if b.Len() == 0 {
		b.WriteString("No open questions or topics yet: look over the recent changes for anything worth raising.")
	}
	return strings.TrimSpace(b.String())
}

// openQuestions are the dot's newest active question entries.
func (e *Engine) openQuestions(project string, d dots.Dot) []memory.Entry {
	if project == "" || !e.memOn(d) {
		return nil
	}
	qs, err := e.d.Memory.List(project, memory.Filter{Scope: memory.ScopeDot, Dot: d.Slug,
		Kinds: []memory.Kind{memory.KindQuestion}, Statuses: []memory.Status{memory.StatusActive}})
	if err != nil {
		e.d.Logf("dot %s: open questions: %v", d.Slug, err)
	}
	sort.SliceStable(qs, func(i, j int) bool { return qs[i].CreatedAt.After(qs[j].CreatedAt) })
	return qs[:min(len(qs), researchQuestions)]
}

// addAway collects a relevant research result (j.deliver). The pane and its
// generation are re-checked under e.mu, so a closed or reused pane never gets
// the part. Observe dots learn silently: their results stay in memory and
// never make a card.
func (e *Engine) addAway(j bgJob, v backend.Verdict) {
	if j.dot.Delivery.Mode == dots.ModeObserve {
		return
	}
	e.mu.Lock()
	st := e.idle[j.paneID]
	if st == nil || e.bgPaneGoneLocked(j) { // a closed or reused pane gets no digest part
		e.mu.Unlock()
		return
	}
	if len(st.away) == 0 || j.since.Before(st.awaySince) {
		st.awaySince = j.since
	}
	st.away = append(st.away, awayPart{slug: j.dot.Slug, name: j.dot.Name, glyph: j.dot.Glyph, v: v})
	flush := v.Severity == "critical"
	if st.period != j.period || st.flushWanted { // the developer is back
		if st.inflight > 1 {
			st.flushWanted = true // more results are coming: one digest
		} else {
			flush = true
		}
	}
	e.mu.Unlock()
	if flush {
		e.flushAway(j.paneID)
	}
	e.broadcast()
}

// researchDone ends one reserved research run (j.done, exactly once): it
// releases the dedupe reservation, and the last run in flight flushes a
// digest the developer is waiting for. A run whose pane closed (or whose id
// now names a new attachment) reserved nothing in the current idle state.
func (e *Engine) researchDone(j bgJob) {
	paneID := j.paneID
	k := key{paneID, j.dot.Slug}
	e.mu.Lock()
	if e.bgQueued[k]--; e.bgQueued[k] <= 0 {
		delete(e.bgQueued, k)
	}
	st := e.idle[paneID]
	if st == nil || e.gens[paneID] != j.gen {
		e.mu.Unlock()
		return
	}
	st.inflight--
	flush := st.inflight == 0 && st.flushWanted && len(st.away) > 0
	if st.inflight == 0 && len(st.away) == 0 {
		st.flushWanted = false
	}
	e.mu.Unlock()
	if flush {
		e.flushAway(paneID)
	}
}

// flushAway turns the collected research into one digest card (Phase 3's
// plain merge, no chair call). It is a queue card: never auto-delivered. The
// card is added with the pane check in the same e.mu section (e.mu ->
// stores), as deliverScheduled does, so a pane closed or reused meanwhile
// never gets it.
func (e *Engine) flushAway(paneID string) {
	e.mu.Lock()
	st := e.idle[paneID]
	if st == nil || len(st.away) == 0 {
		e.mu.Unlock()
		return
	}
	parts, since, gen := st.away, st.awaySince, e.gens[paneID]
	st.away, st.flushWanted = nil, false
	e.mu.Unlock()
	now := e.now()
	cparts := make([]council.Part, 0, len(parts))
	qparts := make([]queue.CardPart, 0, len(parts))
	for _, p := range parts {
		cparts = append(cparts, council.Part{Slug: p.slug, Name: p.name, Glyph: p.glyph, Verdict: p.v})
		qparts = append(qparts, queue.CardPart{Dot: p.slug, DotName: p.name, Glyph: p.glyph, Severity: p.v.Severity, Body: p.v.Message})
	}
	noun := "dots"
	if len(parts) == 1 {
		noun = "dot"
	}
	label := fmt.Sprintf("%d %s · %d min", len(parts), noun, max(1, int(now.Sub(since).Round(time.Minute)/time.Minute)))
	c := queue.Card{Kind: queue.KindDigest, Dot: digestSlug, DotName: digestName, Glyph: digestGlyph, PaneID: paneID,
		Severity: council.MaxSeverity(cparts), Body: council.PlainMerge(cparts), Mode: string(dots.ModeQueue),
		Parts: qparts, Note: "researched while you were away", Origin: state.RunResearch, Label: label}
	e.hook("away-before-add")
	e.mu.Lock()
	att, ok := e.d.Attachments.Get(paneID)
	if !ok || e.gens[paneID] != gen {
		e.mu.Unlock()
		e.d.Logf("pane %s closed before its away digest; dropped", paneID)
		return
	}
	c.Project = att.ProjectID
	_, err := e.d.Queue.Add(c)
	var ferr error
	if err == nil {
		ferr = e.d.Feed.Append(state.FeedEntry{Time: time.Now(), PaneID: paneID, Dot: digestSlug, Outcome: state.OutcomeResearch, Detail: "digest: " + label})
	}
	e.mu.Unlock()
	if err != nil {
		e.d.Logf("queue add: %v", err)
		return
	}
	if ferr != nil {
		e.d.Logf("feed: %v", ferr)
	}
	e.reportPending(paneID)
	e.broadcast()
}

// awayLocked is the snapshot's view of collected research. e.mu held.
func (e *Engine) awayLocked(paneID string) *view.AwayState {
	st := e.idle[paneID]
	if st == nil || len(st.away) == 0 {
		return nil
	}
	return &view.AwayState{Since: st.awaySince, Pending: len(st.away)}
}

// researchNextLocked is the roster hint "research in 6m". e.mu held.
func (e *Engine) researchNextLocked(d dots.Dot, paneID string, now time.Time) string {
	st := e.idle[paneID]
	if !d.Research.Enabled || st == nil || !st.agentIdle || st.ran[d.Slug] == st.period {
		return ""
	}
	left := time.Duration(d.Research.IdleMinutes)*time.Minute - now.Sub(st.activeAt)
	if left <= 0 {
		return ""
	}
	return fmt.Sprintf("research in %dm", int(math.Ceil(left.Minutes())))
}
