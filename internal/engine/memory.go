package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// pruneInterval is how often attached projects' memory is pruned.
const pruneInterval = 24 * time.Hour

// suppressWindow is how many turns an accepted finding suppresses repeats for.
const suppressWindow = 50

// MemoryInput is what DecideMemory looks at for an observe-mode dot.
type MemoryInput struct {
	Dot           dots.Dot
	Att           state.DotAttachment
	NewTurns      []transcript.Turn // turns since Att.MemorySeenTurn
	PausedAll     bool
	MemoryEnabled bool // config memory_enabled
	DotCalls      int
	PaneCalls     int
	PaneBudget    int

	// Dollar budgets (Phase 4), as in FilterInput; a budget <= 0 is off.
	DotCostUSD    float64
	PaneCostUSD   float64
	DotBudgetUSD  float64
	PaneBudgetUSD float64
}

// DecideMemory reports whether an observe-mode dot should run a memory pass
// now (spec §6.6): memory and learning on, not paused, every_turns new turns
// that aren't all its own, passes and call budgets left.
func DecideMemory(in MemoryInput) bool {
	m := in.Dot.Memory
	switch {
	case !in.MemoryEnabled, !m.Enabled, !m.Learn, in.PausedAll, in.Att.Paused:
		return false
	case len(in.NewTurns) < m.EveryTurns:
		return false
	case in.Att.MemoryPasses >= m.MaxPassesPerSession:
		return false
	case in.DotCalls >= in.Dot.Budget.MaxCallsPerSession, in.PaneCalls >= in.PaneBudget:
		return false
	case OverUSD(in.DotCostUSD, in.DotBudgetUSD), OverUSD(in.PaneCostUSD, in.PaneBudgetUSD):
		return false
	}
	for _, t := range in.NewTurns {
		if !t.IsFromDot(in.Dot.Name) {
			return true
		}
	}
	return false
}

// memRun is the memory side of one run.
type memRun struct {
	project string         // "" = no memory for this run
	block   string         // rendered MEMORY block
	learn   bool           // the dot may write memory
	refs    []memory.Ref   // entries in the block (touched after the run)
	cands   []memory.Entry // the dot's dismissed and finding entries (anti-repeat)
}

// memEnabled reports whether memory is on right now: a store exists and
// config.toml's memory_enabled (re-read at runtime) is true. Every memory read
// and write checks it at the point of use.
func (e *Engine) memEnabled() bool {
	return e.d.Memory != nil && e.config().MemoryEnabled
}

func (e *Engine) memOn(d dots.Dot) bool {
	return e.memEnabled() && d.Memory.Enabled
}

// loadMemory builds the run's memory block. A lock timeout means this run
// goes without memory; writes after the run are still attempted.
func (e *Engine) loadMemory(att *state.Attachment, d dots.Dot, turns []transcript.Turn) memRun {
	if !e.memOn(d) {
		return memRun{}
	}
	pid := e.projectOf(att)
	if pid == "" {
		return memRun{}
	}
	mr := memRun{project: pid, learn: d.Memory.Learn}
	entries, err := e.d.Memory.Candidates(pid, d.Slug, d.Memory.ReadProject)
	if err != nil {
		e.d.Logf("memory for dot %s on %s: %v", d.Slug, att.PaneID, err)
		if errors.Is(err, memory.ErrLocked) {
			return mr
		}
	}
	q := memory.Query{Dot: d.Slug, Paths: touchedPaths(turns), Text: turnsText(turns), Now: time.Now()}
	sel := memory.Select(entries, q, d.Memory.PromptChars)
	mr.block = memory.Render(sel)
	for _, s := range sel {
		mr.refs = append(mr.refs, s.Ref())
	}
	for _, en := range entries {
		if en.Scope == memory.ScopeDot && (en.Kind == memory.KindDismissed || en.Kind == memory.KindFinding) {
			mr.cands = append(mr.cands, en)
		}
	}
	return mr
}

// projectOf returns the attachment's project id, resolving it once for
// attachments saved before memory existed.
func (e *Engine) projectOf(att *state.Attachment) string {
	if att.ProjectID != "" || att.PaneID == "" || !e.memEnabled() {
		return att.ProjectID
	}
	pr := e.resolve(att.Cwd)
	if pr.ID == "" {
		return ""
	}
	att.ProjectID, att.ProjectName = pr.ID, pr.Name
	_ = e.d.Attachments.Update(att.PaneID, func(a *state.Attachment) {
		if a.ProjectID == "" {
			a.ProjectID, a.ProjectName = pr.ID, pr.Name
		}
	})
	e.seeProject(pr)
	return pr.ID
}

func (e *Engine) resolve(cwd string) memory.Project {
	if e.d.ResolveProject == nil || cwd == "" {
		return memory.Project{}
	}
	pr, err := e.d.ResolveProject(cwd)
	if err != nil {
		e.d.Logf("project for %s: %v", cwd, err)
	}
	if !memory.ValidProjectID(pr.ID) {
		return memory.Project{}
	}
	return pr
}

// reresolve follows a pane into a new agent session (maybe a new directory).
func (e *Engine) reresolve(paneID, cwd string) {
	if !e.memEnabled() {
		return
	}
	pr := e.resolve(cwd)
	if pr.ID == "" {
		return
	}
	if err := e.d.Attachments.Update(paneID, func(a *state.Attachment) {
		a.ProjectID, a.ProjectName = pr.ID, pr.Name
	}); err != nil {
		return
	}
	e.seeProject(pr)
}

func (e *Engine) seeProject(pr memory.Project) {
	if err := retryLocked(func() error { return e.d.Memory.SeeProject(pr) }); err != nil {
		e.d.Logf("memory: record project %s: %v", pr.ID, err)
	}
	e.refreshStats(pr.ID)
	e.broadcast()
}

func retryLocked(f func() error) error {
	err := f()
	if errors.Is(err, memory.ErrLocked) {
		err = f()
	}
	return err
}

func (e *Engine) touchMemory(mr memRun) {
	if mr.project == "" || len(mr.refs) == 0 || !e.memEnabled() {
		return
	}
	if err := retryLocked(func() error { return e.d.Memory.Touch(mr.project, mr.refs) }); err != nil {
		e.memWriteFailed(err)
	}
}

// applyMemoryOps applies the model's remember ops and logs "learned N". The
// per-reply cap (3 for a review, 5 for a memory pass) is enforced by
// backend.ParseReply and backend.ParseMemoryOps; it is enforced again here so
// no backend can get more than its cap into the store from one reply.
func (e *Engine) applyMemoryOps(paneID string, d dots.Dot, turn int, mr memRun, ops []backend.MemoryOp, att state.Attachment, memoryPass bool, opts ...memory.ApplyOptions) int {
	if mr.project == "" || !mr.learn || len(ops) == 0 || !e.memEnabled() {
		return 0
	}
	limit := backend.MaxReviewOps
	if memoryPass {
		limit = backend.MaxPassOps
	}
	if len(ops) > limit {
		e.d.Logf("dot %s: %d memory ops in one reply; keeping the first %d", d.Slug, len(ops), limit)
		ops = ops[:limit]
	}
	ev := memory.Evidence{Session: att.SessionValue, Pane: paneID, Turn: turn}
	var res memory.ApplyResult
	err := retryLocked(func() error {
		var err error
		res, err = e.d.Memory.Apply(mr.project, d.Slug, ops, ev, opts...)
		return err
	})
	for _, why := range res.Rejected {
		e.d.Logf("dot %s: memory op rejected: %s", d.Slug, why)
	}
	if err != nil {
		e.memWriteFailed(err)
	}
	n := res.Applied()
	if n > 0 {
		e.feed(paneID, d.Slug, turn, state.OutcomeLearned, fmt.Sprintf("learned %d", n))
		e.refreshStats(mr.project)
	}
	return n
}

// suppressedBy returns the ref of a remembered entry the message repeats: an
// active dismissed entry, or a finding accepted in this session within the
// last suppressWindow turns. Only sent findings (Source outcome) and findings
// the developer saved (Source user) count (spec M10): a model-written finding
// must never silence its own dot (§8.2).
func suppressedBy(mr memRun, msg string, turn int, session string) (string, bool) {
	best, ref := 0.0, ""
	for _, en := range mr.cands {
		if en.Kind != memory.KindFinding && en.Kind != memory.KindDismissed {
			continue // research and question entries never suppress
		}
		if en.Kind == memory.KindFinding {
			if en.Source != memory.SourceOutcome && en.Source != memory.SourceUser {
				continue
			}
			ev := en.Evidence
			if ev.Session == "" || ev.Session != session || ev.Turn <= 0 || turn < ev.Turn || turn-ev.Turn > suppressWindow {
				continue
			}
		}
		if sc := memory.SuppressScore(msg, en.Text); sc > best {
			best, ref = sc, en.Ref().String()
		}
	}
	return ref, best >= memory.SuppressThreshold
}

// memWriteFailed logs a memory write failure; anything but a lock timeout
// also gets a one-time toast.
func (e *Engine) memWriteFailed(err error) {
	e.d.Logf("memory write failed: %v", err)
	if errors.Is(err, memory.ErrLocked) {
		return
	}
	e.mu.Lock()
	first := !e.memToast
	e.memToast = true
	e.mu.Unlock()
	if first {
		if nerr := e.d.Herdr.Notify(e.ctx, "Herdr Dots", "memory write failed: "+err.Error()); nerr != nil {
			e.d.Logf("notify: %v", nerr)
		}
	}
}

func (e *Engine) refreshStats(pid string) {
	if !e.memEnabled() || pid == "" {
		return
	}
	st, err := e.d.Memory.Stats(pid)
	if err != nil {
		e.d.Logf("memory stats %s: %v", pid, err)
		if errors.Is(err, memory.ErrLocked) {
			return // keep the last good counts
		}
	}
	e.mu.Lock()
	e.memStats[pid] = st
	e.mu.Unlock()
}

func (e *Engine) setLearning(k key, on bool) {
	e.mu.Lock()
	if on {
		e.learning[k] = true
	} else {
		delete(e.learning, k)
	}
	e.mu.Unlock()
	e.broadcast()
}

// clearError sets an error status back to idle after a successful memory
// pass (memory passes set no status of their own, so nothing else would).
func (e *Engine) clearError(k key) {
	e.mu.Lock()
	cleared := e.status[k] == view.StatusError
	if cleared {
		e.status[k] = view.StatusIdle
	}
	e.mu.Unlock()
	if cleared {
		e.broadcast()
	}
}

// spawnOutcome records what happened to a card, off the caller's locks.
func (e *Engine) spawnOutcome(c queue.Card, kind string) { e.spawnOutcomeOf(c, kind, false) }

// spawnOutcomeOf is spawnOutcome for a card whose text held a secret that was
// redacted before it reached the card (secret: a redacted ledger line, no
// entry).
func (e *Engine) spawnOutcomeOf(c queue.Card, kind string, secret bool) {
	e.mu.Lock()
	orig, edited := e.origBody[c.ID]
	delete(e.origBody, c.ID)
	e.mu.Unlock()
	if !e.memEnabled() {
		return
	}
	e.spawn(func() { e.recordOutcome(c, kind, orig, edited, secret) })
}

func (e *Engine) recordOutcome(c queue.Card, kind, orig string, edited, secret bool) {
	if c.Kind == queue.KindCouncil || c.Kind == queue.KindDigest {
		e.recordCouncilOutcomes(c, kind, edited) // one outcome per contributing member or researching dot (council_memory.go)
		return
	}
	e.mu.Lock()
	d, known := e.dots[c.Dot]
	e.mu.Unlock()
	if known && !d.Memory.Enabled {
		return
	}
	pid := c.Project
	a, attached := e.d.Attachments.Get(c.PaneID)
	if pid == "" && attached {
		pid = e.projectOf(&a)
	}
	if pid == "" {
		e.d.Logf("card %s: project unknown; outcome not remembered", c.ID)
		return
	}
	o := memory.Outcome{Dot: c.Dot, Card: c.ID, Kind: kind, Severity: c.Severity, Body: c.Body, Turn: c.Turn, Pane: c.PaneID, Secret: secret}
	if attached {
		o.Session = a.SessionValue
	}
	if kind == memory.OutcomeSent && edited {
		o.Kind, o.Body, o.Edited = memory.OutcomeEdited, orig, c.Body
	}
	if err := retryLocked(func() error { return e.d.Memory.RecordOutcome(pid, o) }); err != nil {
		e.memWriteFailed(err)
		return
	}
	e.refreshStats(pid)
	e.broadcast()
}

// Remember turns a card into a memory entry the developer chose (Dots pane M).
func (e *Engine) Remember(cardID string, as memory.RememberAs, reason string) error {
	if !e.memEnabled() {
		return errors.New("memory is turned off (memory_enabled = false)")
	}
	if !as.Valid() {
		return fmt.Errorf("remember as %q: want finding, rule, convention or global", as)
	}
	c, ok := e.d.Queue.Get(cardID)
	if !ok {
		return fmt.Errorf("card %s: %w", cardID, queue.ErrNotFound)
	}
	if c.Kind == queue.KindPatch {
		return fmt.Errorf("card %s: %w", cardID, ErrPatchNotRemembered)
	}
	if (c.Kind == queue.KindCouncil || c.Kind == queue.KindDigest) && as != memory.AsConvention {
		return fmt.Errorf("a %s card can only be remembered as a project convention (c)", c.Kind)
	}
	ev := memory.Evidence{Pane: c.PaneID, Turn: c.Turn, Card: c.ID}
	pid := c.Project
	if a, ok := e.d.Attachments.Get(c.PaneID); ok {
		// The session lets a saved finding suppress repeats (spec M10).
		ev.Session = a.SessionValue
		if pid == "" {
			pid = e.projectOf(&a)
		}
	}
	if pid == "" && as != memory.AsGlobal {
		return fmt.Errorf("card %s: its project is unknown", cardID)
	}
	text := c.Body
	if c.Kind == queue.KindCouncil || c.Kind == queue.KindDigest {
		text = councilConventionText(c) // the digest starts with a "## " header
		if text == council.NoPoint {
			return fmt.Errorf("card %s: the %s digest has no point to remember", cardID, c.Kind)
		}
	}
	in := memory.RememberInput{As: as, Dot: c.Dot, Text: text, Reason: reason, Ev: ev}
	if _, err := e.d.Memory.Remember(pid, in); err != nil {
		return err
	}
	e.refreshStats(pid)
	e.broadcast()
	return nil
}

// MemoryChanged refreshes memory counts after a UI or the CLI edited memory.
func (e *Engine) MemoryChanged() {
	if !e.memEnabled() {
		return
	}
	for _, pid := range e.attachedProjects() {
		e.refreshStats(pid)
	}
	e.broadcast()
}

func (e *Engine) attachedProjects() []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range e.d.Attachments.List() {
		if a.ProjectID != "" && !seen[a.ProjectID] {
			seen[a.ProjectID] = true
			out = append(out, a.ProjectID)
		}
	}
	return out
}

// memoryLoop prunes attached projects at start and every pruneInterval. It
// runs whenever a store exists: config.toml reloads at runtime, so
// maintainMemory reads memory_enabled on every pass.
func (e *Engine) memoryLoop() {
	e.maintainMemory()
	t := time.NewTicker(pruneInterval)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			e.maintainMemory()
		}
	}
}

func (e *Engine) maintainMemory() {
	if !e.memEnabled() {
		return
	}
	for _, pid := range e.attachedProjects() {
		rep, err := e.d.Memory.Prune(pid, time.Now())
		switch {
		case err != nil:
			e.d.Logf("memory prune %s: %v", pid, err)
		case rep != (memory.PruneReport{}):
			e.d.Logf("memory prune %s: archived %d, deleted %d, ledger trimmed %d", pid, rep.Archived, rep.Deleted, rep.LedgerTrimmed)
		}
		e.refreshStats(pid)
	}
	e.broadcast()
}

func touchedPaths(turns []transcript.Turn) []string {
	var out []string
	for _, t := range turns {
		out = append(out, t.TouchedPaths()...)
	}
	return out
}

// turnsText is the turns' text for keyword scoring, capped.
func turnsText(turns []transcript.Turn) string {
	var b strings.Builder
	for _, t := range turns {
		b.WriteString(t.User)
		b.WriteByte('\n')
		b.WriteString(t.Assistant)
		b.WriteByte('\n')
		if b.Len() > 40000 {
			break
		}
	}
	return b.String()
}
