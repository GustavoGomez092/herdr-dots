package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// ErrCouncilRunning: one council at a time per pane.
var ErrCouncilRunning = errors.New("council already running")

// errRunNotStarted is what a council sink gets when runDot returns before
// running the backend for a reason that left no error of its own.
var errRunNotStarted = errors.New("run did not start")

// councilSlug keys the council in the feed, the usage counters (the chair
// counts against the pane budget only) and the card's Dot field.
const councilSlug = dots.ReservedSlug

// maxQuestionChars caps the council question.
const maxQuestionChars = 2000

// councilRun is one council in progress. The fields below the line are
// guarded by e.mu; the rest never change after Council returns.
type councilRun struct {
	id       string
	paneID   string
	question string
	rounds   int
	turn     int               // the newest turn when the council started
	turns    []transcript.Turn // the transcript at that point
	members  []dots.Dot
	skipped  []council.Part // ineligible members: "not heard from"
	ctx      context.Context
	cancel   context.CancelFunc

	phase     string
	round     int
	done      int
	total     int
	abandoned bool
}

type memberResult struct {
	i   int
	res backend.Result
	err error
}

// CouncilPreflight computes who would sit on a council for the pane and what
// it would cost, and publishes the result in the pane's snapshot
// (view.Pane.CouncilPreflight). nil slugs means every attached dot;
// an explicitly empty selection leaves no members and is refused.
func (e *Engine) CouncilPreflight(paneID string, slugs []string) (council.Preflight, error) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return council.Preflight{}, fmt.Errorf("%w: %s", state.ErrNoAttachment, paneID)
	}
	pf, _, _ := e.councilPlan(att, slugs)
	e.mu.Lock()
	if e.preflights == nil {
		e.preflights = map[string]council.Preflight{}
	}
	e.preflights[paneID] = pf
	e.mu.Unlock()
	e.broadcast()
	return pf, nil
}

// councilPlan returns the preflight, the member dots and a failed part for
// every skipped member.
func (e *Engine) councilPlan(att state.Attachment, slugs []string) (council.Preflight, []dots.Dot, []council.Part) {
	cfg := e.config()
	cc := cfg.Council.Normalized()
	e.mu.Lock()
	pausedAll := e.pausedAll
	known := map[string]dots.Dot{}
	for _, da := range att.Dots {
		if d, ok := e.dots[da.Slug]; ok {
			known[da.Slug] = d
		}
	}
	e.mu.Unlock()
	cands := make([]council.Candidate, 0, len(att.Dots))
	for _, da := range att.Dots {
		d, ok := known[da.Slug]
		c := council.Candidate{Slug: da.Slug, Paused: da.Paused || pausedAll, Unknown: !ok}
		if ok {
			c.CallsLeft = callsLeft(d.Budget.MaxCallsPerSession, e.d.Usage.Calls(att.PaneID, da.Slug))
		}
		cands = append(cands, c)
	}
	pf := council.Plan(council.PlanInput{
		Candidates: cands, Rounds: cc.Rounds, MaxMembers: cc.MaxMembers,
		PaneLeft: callsLeft(cfg.SessionBudgetCalls, e.d.Usage.PaneCalls(att.PaneID)), Chair: cc.Merge == config.MergeChair,
	})
	if slugs != nil { // nil = every eligible dot; an explicit empty selection leaves no members
		pf = pf.Select(slugs)
	}
	members := make([]dots.Dot, 0, len(pf.Members))
	for _, s := range pf.Members {
		members = append(members, known[s])
	}
	var skipped []council.Part
	for _, s := range pf.Skipped {
		p := council.Part{Slug: s, Name: s, Glyph: "?", Err: "skipped: " + pf.Reasons[s]}
		if d, ok := known[s]; ok {
			p.Name, p.Glyph = d.Name, d.Glyph
		}
		skipped = append(skipped, p)
	}
	return pf, members, skipped
}

// callsLeft is what remains of a call budget (never negative). As in v1, a
// budget of 0 means no calls; config and dot validation clamp budgets to 1 or
// more anyway.
func callsLeft(budget, used int) int {
	return max(0, budget-used)
}

// Council asks every eligible attached dot (or the listed ones) the same
// question in parallel and turns their answers into one council card. It
// returns the council id at once; the council runs in the background.
func (e *Engine) Council(ctx context.Context, paneID, question string, slugs []string) (string, error) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return "", fmt.Errorf("%w: %s", state.ErrNoAttachment, paneID)
	}
	if e.councilBusy(paneID) {
		return "", ErrCouncilRunning
	}
	pf, members, skipped := e.councilPlan(att, slugs)
	if !pf.Allowed {
		return "", errors.New(pf.Refusal)
	}
	turns, err := e.reader(att).Turns(ctx)
	if err != nil {
		return "", fmt.Errorf("read conversation: %w", err)
	}
	q := []rune(strings.TrimSpace(textutil.SanitizeModelText(question)))
	if len(q) > maxQuestionChars {
		q = q[:maxQuestionChars]
	}
	run := &councilRun{paneID: paneID, question: string(q), rounds: pf.Rounds, turns: turns, members: members, skipped: skipped}
	if len(turns) > 0 {
		run.turn = turns[len(turns)-1].Index
	}
	run.ctx, run.cancel = context.WithCancel(e.ctx)
	e.mu.Lock()
	if e.councils[paneID] != nil {
		e.mu.Unlock()
		run.cancel()
		return "", ErrCouncilRunning
	}
	if e.councils == nil {
		e.councils = map[string]*councilRun{}
	}
	e.councilSeq++
	run.id = fmt.Sprintf("council-%d", e.councilSeq)
	run.phase, run.round, run.total = view.CouncilMembers, 1, len(members)
	e.councils[paneID] = run
	delete(e.preflights, paneID)
	e.mu.Unlock()
	// The pane may have closed since the first check; dropPane could not find
	// this council yet.
	if _, ok := e.d.Attachments.Get(paneID); !ok {
		e.abandonCouncil(paneID)
		e.broadcast()
		return "", fmt.Errorf("%w: %s", state.ErrNoAttachment, paneID)
	}
	e.broadcast()
	e.spawn(func() { e.runCouncil(run) })
	return run.id, nil
}

func (e *Engine) councilBusy(paneID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.councils[paneID] != nil
}

// runCouncil runs round 1, the optional round 2 and the merge, then adds the
// card. Members share one deadline (run timeout × rounds); the chair gets one
// more run timeout.
func (e *Engine) runCouncil(run *councilRun) {
	defer e.endCouncil(run)
	mctx, cancel := context.WithTimeout(run.ctx, e.councilMemberWait(run.rounds))
	defer cancel()
	parts := e.councilRound(mctx, run, 1, nil)
	if run.rounds >= 2 && council.Answered(parts) >= 2 && mctx.Err() == nil {
		parts = e.councilRound(mctx, run, 2, parts)
	}
	if e.councilLost(run) {
		return
	}
	if council.Answered(parts) == 0 {
		const msg = "council failed: no member answered"
		e.feed(run.paneID, councilSlug, run.turn, state.OutcomeError, msg)
		if err := e.d.Herdr.Notify(e.ctx, transcript.CouncilGlyph+" "+transcript.CouncilName+" dot", msg); err != nil {
			e.d.Logf("notify: %v", err)
		}
		return
	}
	all := append(parts, run.skipped...)
	e.setCouncilProgress(run, view.CouncilMerging, run.rounds, 0, 0)
	body, note := e.mergeCouncil(run, all)
	if e.councilLost(run) {
		return
	}
	e.addCouncilCard(run, all, body, note)
}

// councilMemberWait bounds every member run of a council.
func (e *Engine) councilMemberWait(rounds int) time.Duration {
	if e.councilWait > 0 {
		return e.councilWait
	}
	return e.config().RunTimeout() * time.Duration(max(1, rounds))
}

// councilRound runs one round through runDot (and so through sem) and
// collects the answers. Round 1 runs every member; round 2 runs the members
// that answered round 1, each with the others' answers. A member still
// running at the deadline fails in round 1 ("timed out") and keeps its
// round-1 answer in round 2.
func (e *Engine) councilRound(ctx context.Context, run *councilRun, round int, prev []council.Part) []council.Part {
	parts := make([]council.Part, len(run.members))
	if round == 1 {
		for i, d := range run.members {
			parts[i] = council.Part{Slug: d.Slug, Name: d.Name, Glyph: d.Glyph}
		}
	} else {
		copy(parts, prev)
	}
	var idx []int
	for i := range run.members {
		if round == 1 || !prev[i].Failed() {
			idx = append(idx, i)
		}
	}
	phase := view.CouncilMembers
	if round == 2 {
		phase = view.CouncilRound2
	}
	e.setCouncilProgress(run, phase, round, 0, len(idx))
	results := make(chan memberResult, len(idx)) // buffered: a late sink never blocks
	for _, i := range idx {
		d := run.members[i]
		q := run.question
		if round == 2 {
			q = council.RoundTwoQuestion(run.question, prev, d.Slug)
		}
		turns := tail(run.turns, d.Context.Turns)
		o := runOpts{summon: true, noForce: true, question: q, ctx: ctx,
			sink: func(res backend.Result, err error) { results <- memberResult{i: i, res: res, err: err} }}
		e.spawn(func() { e.runDot(run.paneID, d, turns, o) })
	}
	got := map[int]bool{}
	for len(got) < len(idx) {
		select {
		case r := <-results:
			got[r.i] = true
			applyMember(ctx, &parts[r.i], round, r)
			e.setCouncilProgress(run, phase, round, len(got), len(idx))
		case <-ctx.Done():
			for _, i := range idx {
				if !got[i] && round == 1 {
					parts[i].Err = "timed out"
				}
			}
			return parts
		}
	}
	return parts
}

// applyMember stores one member's answer. Model text is sanitized here too,
// whatever the backend did.
func applyMember(ctx context.Context, p *council.Part, round int, r memberResult) {
	if r.err != nil {
		if round == 1 {
			p.Err = failText(ctx, r.err)
		}
		return
	}
	v := r.res.Verdict
	v.Message = strings.TrimSpace(textutil.SanitizeModelText(v.Message))
	if round == 1 {
		p.Verdict = v
		return
	}
	if !v.Relevant || v.Message == "" {
		return // round-2 pass: no further comment
	}
	stance, rest := council.ParseStance(v.Message)
	if stance == "" {
		stance = council.StanceAdd // something said without a prefix is an addition
	}
	p.Stance, p.Reply, p.ReplySeverity = stance, rest, v.Severity
}

// failText is how a failed member shows on the card.
func failText(ctx context.Context, err error) string {
	if timedOut(ctx, err) {
		return "timed out"
	}
	return "error: " + errText(err)
}

func timedOut(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil
}

// errText is an error as one sanitized line of at most 120 runes.
func errText(err error) string {
	msg := []rune(strings.Join(strings.Fields(textutil.SanitizeModelText(err.Error())), " "))
	if len(msg) > 120 {
		msg = append(msg[:120], '…')
	}
	return string(msg)
}

// mergeCouncil builds the card body: the chair's digest when two or more
// members contributed and merge = "chair", else (or when the chair fails) the
// plain merge. note is the card footer.
func (e *Engine) mergeCouncil(run *councilRun, parts []council.Part) (body, note string) {
	cc := e.config().Council.Normalized()
	if cc.Merge == config.MergePlain || council.Contributing(parts) < 2 {
		return council.PlainMerge(parts), ""
	}
	digest, err := e.runChair(run, parts, cc)
	if err != nil {
		e.d.Logf("council %s on %s: chair: %v", run.id, run.paneID, err)
		why := errText(err)
		if timedOut(context.Background(), err) {
			why = "timed out"
		}
		return council.PlainMerge(parts), "merged without chair (chair error: " + why + ")"
	}
	body, verified := council.ParseDigest(digest)
	body = council.WithNotHeard(body, parts)
	if !verified {
		note = "unverified digest: some points have no attribution (space shows each answer)"
	}
	return body, note
}

// runChair merges the answers through any backend, with the verdict
// contract: the digest comes back as the verdict message. The chair is
// read-only (no repo access), sees project memory only (council_memory.go)
// and counts against the pane budget, never a dot's.
func (e *Engine) runChair(run *councilRun, parts []council.Part, cc config.CouncilConfig) (string, error) {
	cfg := e.config()
	kind, model := cc.ChairBackend, cc.ChairModel
	if kind == "" {
		kind = cfg.DefaultBackend
	}
	if model == "" {
		model = cfg.DefaultModel
	}
	be, err := e.d.Backend(kind)
	if err != nil {
		return "", err
	}
	n := 1
	for _, d := range run.members {
		n = max(n, d.Context.Turns)
	}
	turns := tail(run.turns, n)
	att, _ := e.d.Attachments.Get(run.paneID)
	req := backend.Request{
		DotName: council.ChairName, Purpose: council.ChairPurpose, Verbosity: "normal", Model: model,
		Cwd: att.Cwd, Turns: turns, Question: council.ChairQuestion(run.question, parts), Force: true,
		Memory: e.chairMemory(&att, turns),
	}
	select { // the chair shares max_parallel_runs with the members
	case e.sem <- struct{}{}:
	case <-run.ctx.Done():
		return "", run.ctx.Err()
	}
	defer func() { <-e.sem }()
	ctx, cancel := context.WithTimeout(run.ctx, cfg.RunTimeout())
	defer cancel()
	res, err := be.Run(ctx, req)
	_ = e.d.Usage.Record(run.paneID, councilSlug, res.InputTokens, res.OutputTokens)
	if err != nil {
		return "", err
	}
	if !res.Verdict.Relevant || strings.TrimSpace(textutil.SanitizeModelText(res.Verdict.Message)) == "" {
		return "", errors.New("the chair returned no digest")
	}
	return strings.TrimSpace(textutil.SanitizeModelText(res.Verdict.Message)), nil
}

// addCouncilCard queues the council card (always queue mode) unless the pane
// is gone.
func (e *Engine) addCouncilCard(run *councilRun, parts []council.Part, body, note string) {
	att, ok := e.d.Attachments.Get(run.paneID)
	if !ok {
		e.d.Logf("council %s: pane %s is gone; no card", run.id, run.paneID)
		return
	}
	c := queue.Card{Kind: queue.KindCouncil, Dot: councilSlug, DotName: transcript.CouncilName, Glyph: transcript.CouncilGlyph,
		PaneID: run.paneID, Turn: run.turn, Severity: council.MaxSeverity(parts), Mode: string(dots.ModeQueue),
		Body: textutil.SanitizeModelText(body), Question: textutil.SanitizeModelText(run.question),
		Parts: cardParts(parts), Note: textutil.SanitizeModelText(note), Project: att.ProjectID}
	if e.councilLost(run) {
		return // the pane closed (or the engine stopped) while the card was built
	}
	if _, err := e.d.Queue.Add(c); err != nil {
		e.d.Logf("queue add: %v", err)
		msg := "council card lost: " + errText(err)
		e.feed(run.paneID, councilSlug, run.turn, state.OutcomeError, msg)
		if nerr := e.d.Herdr.Notify(e.ctx, transcript.CouncilGlyph+" "+transcript.CouncilName+" dot", msg); nerr != nil {
			e.d.Logf("notify: %v", nerr)
		}
		return
	}
	e.feed(run.paneID, councilSlug, run.turn, state.OutcomeCouncil,
		fmt.Sprintf("%d of %d members answered; %s", council.Answered(parts), len(parts), c.Severity))
	e.reportPending(run.paneID)
	e.broadcast()
}

// cardParts turns council parts into the card's per-member answers.
func cardParts(parts []council.Part) []queue.CardPart {
	out := make([]queue.CardPart, 0, len(parts))
	for _, p := range parts {
		// The pane renders card text raw, so every string is sanitized here.
		cp := queue.CardPart{Dot: p.Slug, DotName: textutil.SanitizeModelText(p.Name), Glyph: p.Glyph, Stance: p.Stance}
		switch {
		case p.Failed():
			cp.Failed, cp.Body, cp.Stance = true, textutil.SanitizeModelText(p.Err), ""
		case p.Relevant():
			cp.Severity, cp.Body = p.Verdict.Severity, textutil.SanitizeModelText(p.Verdict.Message)
		}
		if !p.Failed() && p.Reply != "" {
			cp.Body = strings.TrimSpace(cp.Body + "\n↳ " + strings.ToUpper(p.Stance) + ": " + textutil.SanitizeModelText(p.Reply))
			if cp.Severity == "" {
				cp.Severity = p.ReplySeverity
			}
		}
		out = append(out, cp)
	}
	return out
}

func (e *Engine) setCouncilProgress(run *councilRun, phase string, round, done, total int) {
	e.mu.Lock()
	run.phase, run.round = phase, round
	if phase != view.CouncilMerging {
		run.done, run.total = done, total
	}
	e.mu.Unlock()
	e.broadcast()
}

// councilLost reports a council that must produce nothing: its pane closed,
// or the engine is shutting down (an in-flight council is lost; the members
// that were still running would otherwise show up as "timed out").
func (e *Engine) councilLost(run *councilRun) bool {
	return e.ctx.Err() != nil || e.councilAbandoned(run)
}

func (e *Engine) councilAbandoned(run *councilRun) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return run.abandoned
}

// endCouncil forgets the council (unless a newer one replaced it) and stops
// whatever it still runs.
func (e *Engine) endCouncil(run *councilRun) {
	e.mu.Lock()
	if e.councils[run.paneID] == run {
		delete(e.councils, run.paneID)
	}
	e.mu.Unlock()
	run.cancel()
	e.broadcast()
}

// abandonCouncil stops a pane's council because the pane closed: in-flight
// member and chair runs are cancelled and no card is created.
func (e *Engine) abandonCouncil(paneID string) {
	e.mu.Lock()
	run := e.councils[paneID]
	if run != nil {
		run.abandoned = true
		delete(e.councils, paneID)
	}
	delete(e.preflights, paneID)
	e.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// councilViewLocked is the pane's council state for a snapshot. Caller holds e.mu.
func (e *Engine) councilViewLocked(paneID string) (*view.CouncilState, *council.Preflight) {
	var cs *view.CouncilState
	if run := e.councils[paneID]; run != nil {
		cs = &view.CouncilState{ID: run.id, Phase: run.phase, Done: run.done, Total: run.total, Round: run.round}
	}
	var pf *council.Preflight
	if p, ok := e.preflights[paneID]; ok {
		pf = &p
	}
	return cs, pf
}
