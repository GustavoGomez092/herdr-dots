package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/delivery"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
	"herdr-dots/internal/worktree"
)

// Draft errors the Dots pane shows as they are.
var (
	ErrWriteDisabled   = errors.New("write-capable dots are off: set write_enabled = true in config.toml")
	ErrNotWriteCapable = errors.New("this dot can't draft fixes: set write = true under [capabilities] in its dot file")
	ErrDraftLimit      = errors.New("too many drafts are running: wait for one to finish")
	ErrDraftRunning    = errors.New("this dot is already drafting")
	ErrAgentBusy       = errors.New("agent is working; try when idle")
	ErrNotPatch        = errors.New("not a patch card")
	// ErrPatchNotRemembered: a patch card summarizes a diff; it is not advice.
	ErrPatchNotRemembered = errors.New("a patch card can't be remembered: remember the advice card it came from")
)

// maxSnapshotIndex caps a draft's snapshot index (spec §8.1: 500 MB).
const maxSnapshotIndex = 500 << 20

// maxInstruction caps the instruction a draft gets, in runes. A longer one is
// refused, never cut: the developer confirmed the full text.
const maxInstruction = 4000

// draftRun is one draft in progress, guarded by e.mu.
type draftRun struct {
	pane, slug, phase string
	started           time.Time
	cancel            context.CancelFunc // ends the run when its pane goes away
	abandoned         bool               // the pane closed or the dot was detached: no card
	gen               uint64             // the pane's attachment generation when the draft was asked for
}

// Draft starts a write-capable dot on an isolated copy of the pane's
// repository (gate 1, confirmed in the Dots pane). instruction "" with a
// fromCardID drafts that card's advice. It returns the draft id at once;
// the draft runs in the background and ends as a patch card, a feed entry
// or a toast.
func (e *Engine) Draft(ctx context.Context, paneID, slug, instruction, fromCardID string) (string, error) {
	cfg := e.config()
	if cfg.RepoToolsOff() {
		return "", ErrDenyGlobsInvalid
	}
	if !cfg.WriteEnabled {
		return "", ErrWriteDisabled
	}
	if paneID == "" || slug == "" {
		return "", errors.New("a draft needs a pane and a dot")
	}
	if len(instruction) > 8*maxInstruction { // bytes, before any work is done on it
		return "", fmt.Errorf("the instruction is too long (max %d characters)", maxInstruction)
	}
	if e.d.Patches == nil || e.d.WorktreesDir == "" {
		return "", errors.New("drafts are unavailable: the engine has no patch store")
	}
	e.mu.Lock()
	att, ok := e.d.Attachments.Get(paneID)
	gen := e.gens[paneID] // the attachment this draft is asked for
	e.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("%w: %s", state.ErrNoAttachment, paneID)
	}
	if att.Find(slug) == nil {
		return "", fmt.Errorf("dot %q is not attached to %s", slug, paneID)
	}
	e.mu.Lock()
	d, ok := e.paneDotLocked(paneID, slug)
	var berr error
	if ok {
		berr = e.draftBudgetLocked(paneID, d)
	}
	e.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("unknown dot %q", slug)
	}
	if !d.Capabilities.Write || !dots.CanWrite(d.Backend.Kind) {
		return "", ErrNotWriteCapable
	}
	if berr != nil {
		return "", berr
	}
	instruction = strings.TrimSpace(textutil.SanitizeModelText(instruction))
	if instruction == "" && fromCardID != "" {
		c, ok := e.d.Queue.Get(fromCardID)
		if !ok {
			return "", fmt.Errorf("card %s: %w", fromCardID, queue.ErrNotFound)
		}
		if c.PaneID != paneID {
			return "", fmt.Errorf("card %s belongs to pane %s, not %s", fromCardID, c.PaneID, paneID)
		}
		instruction = strings.TrimSpace(textutil.SanitizeModelText(c.Body))
	}
	if instruction == "" {
		return "", errors.New("say what to fix: the instruction is empty")
	}
	if len([]rune(instruction)) > maxInstruction {
		return "", fmt.Errorf("the instruction is too long (max %d characters)", maxInstruction)
	}
	// Git and the transcript are read before any lock is taken.
	repo, err := worktree.RepoRoot(ctx, e.d.Git, att.Cwd)
	if err != nil {
		return "", fmt.Errorf("drafts need a git repository: %w", err)
	}
	turns, err := e.reader(att).Turns(ctx)
	if err != nil {
		return "", fmt.Errorf("read conversation: %w", err)
	}
	turns = tail(turns, d.Context.Turns)
	id := worktree.NewID()
	e.mu.Lock()
	// Git and the transcript were read unlocked: the pane may have closed (and
	// its id been reused) meanwhile. dropPane and Detach flag and bump the
	// generation in the section that deletes the attachment, so this check
	// and the registration below are atomic with them.
	if cur, ok := e.d.Attachments.Get(paneID); !ok || cur.Find(slug) == nil || e.gens[paneID] != gen {
		e.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrPaneClosed, paneID)
	}
	live := 0 // abandoned drafts are winding down and hold no slot
	for _, dr := range e.drafts {
		if dr.abandoned {
			continue
		}
		live++
		if dr.slug == slug {
			e.mu.Unlock()
			return "", ErrDraftRunning
		}
	}
	if live >= max(1, cfg.MaxConcurrentDrafts) {
		e.mu.Unlock()
		return "", ErrDraftLimit
	}
	// Checked again with the draft about to be reserved: asks and drafts
	// that started meanwhile count as calls already made.
	if err := e.draftBudgetLocked(paneID, d); err != nil {
		e.mu.Unlock()
		return "", err
	}
	dctx, cancel := context.WithCancel(e.ctx) // its own context: closing the pane cancels just this draft
	e.drafts[id] = &draftRun{pane: paneID, slug: slug, phase: view.DraftSnapshot, started: time.Now(), cancel: cancel, gen: gen}
	e.mu.Unlock()
	e.broadcast()
	e.spawn(func() { e.runDraft(dctx, id, att, repo, d, instruction, turns) })
	return id, nil
}

// draftBudgetLocked refuses a draft when the dot's or the pane's budget is
// spent: a draft counts as one call (and its cost) like any run, and, as for
// MCP asks (askBlockedLocked), asks, drafts and background runs (Phase 8,
// ruling P8-R2) in flight on the pane count as calls already made. Caller
// holds e.mu (e.mu -> stores).
func (e *Engine) draftBudgetLocked(paneID string, d dots.Dot) error {
	cfg := e.config()
	inFlight := e.inFlightLocked(paneID, key{})
	if e.d.Usage.Calls(paneID, d.Slug) >= d.Budget.MaxCallsPerSession || OverUSD(e.d.Usage.Cost(paneID, d.Slug), d.Budget.MaxUSDPerSession) ||
		e.d.Usage.PaneCalls(paneID)+inFlight >= cfg.SessionBudgetCalls || OverUSD(e.d.Usage.PaneCost(paneID), cfg.SessionBudgetUSD) {
		return fmt.Errorf("%w; resets on a new agent session or press r", ErrBudget)
	}
	return nil
}

func (e *Engine) setDraftPhase(id, phase string) {
	e.mu.Lock()
	if dr, ok := e.drafts[id]; ok {
		dr.phase = phase
	}
	e.mu.Unlock()
	e.broadcast()
}

// denyCheck is the jail's own deny match (case folded) over the built-in
// list, the draft-only list (agent configuration, for every backend) and the
// configured tool_deny_globs, for worktree.Validate.
func (e *Engine) denyCheck() func(rel string) bool {
	deny := append(append(append([]string(nil), toolloop.DefaultDeny...), toolloop.DraftDeny...), e.config().ToolDenyGlobs...)
	return func(rel string) bool { return toolloop.Denied(rel, deny) }
}

// abandonDraftsLocked ends the drafts of a pane that was closed or lost its last
// dot: they are flagged (so they never make a card or a charge, even if the
// pane id is reused) and cancelled. Caller holds e.mu.
func (e *Engine) abandonDraftsLocked(paneID, slug string) {
	for _, dr := range e.drafts {
		if dr.pane == paneID && (slug == "" || dr.slug == slug) && !dr.abandoned {
			dr.abandoned = true
			if dr.cancel != nil {
				dr.cancel()
			}
		}
	}
}

// abandonDrafts abandons the pane's drafts (by one dot, or all with slug "").
func (e *Engine) abandonDrafts(paneID, slug string) {
	e.mu.Lock()
	e.abandonDraftsLocked(paneID, slug)
	e.mu.Unlock()
}

func (e *Engine) draftAbandoned(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	dr, ok := e.drafts[id]
	return !ok || dr.abandoned || e.gens[dr.pane] != dr.gen
}

// chargeDraft charges a draft's usage to its pane unless the pane itself was
// dropped (or its id reused: the generation changed). Detaching one dot does
// not skip it: the backend call happened. The check and the charge happen
// under one e.mu, so a new attachment never pays for the old pane's draft.
func (e *Engine) chargeDraft(id, paneID, slug string, d state.Delta) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if dr, ok := e.drafts[id]; !ok || e.gens[dr.pane] != dr.gen {
		return
	}
	if _, ok := e.d.Attachments.Get(paneID); !ok {
		return
	}
	_ = e.d.Usage.Add(paneID, slug, d)
}

// stopBefore is the tests' stand-in for the process dying before a cleanup
// step: once it fires, every later step is skipped too.
func (e *Engine) stopBefore(crashed *bool, step string) bool {
	if !*crashed && e.cleanupStop != nil && e.cleanupStop(step) {
		*crashed = true
	}
	return *crashed
}

// runDraft snapshots, checks out, runs the dot with write tools in the
// worktree, captures and validates the diff, and stores it as a patch card.
// The record is written first and deleted last (a kill in between leaves what
// the next start's sweep needs to find the ref and the worktree); the
// worktree is always removed; the snapshot ref stays only behind a card.
func (e *Engine) runDraft(ctx context.Context, id string, att state.Attachment, repo string, d dots.Dot, instruction string, turns []transcript.Turn) {
	defer func() {
		e.mu.Lock()
		if dr, ok := e.drafts[id]; ok && dr.cancel != nil {
			dr.cancel()
		}
		delete(e.drafts, id)
		e.mu.Unlock()
		e.broadcast()
	}()
	g := e.d.Git
	turn := 0
	if len(turns) > 0 {
		turn = turns[len(turns)-1].Index
	}
	quiet := func() bool { return e.ctx.Err() != nil || e.draftAbandoned(id) }
	// Phase 9: a draft that fails before its backend call and ends quietly
	// (pane gone, engine stopping) leaves no record; once the call happened it
	// is recorded, "dropped" when it ends quietly (runTrace.usage).
	tr := e.trace(att.PaneID, d, history.KindDraft, att.ProjectID)
	defer tr.done()
	fail := func(err error) {
		if quiet() {
			// The engine is stopping or the pane is gone: nobody to tell.
			e.d.Logf("draft %s by %s on %s ended: %v", id, d.Slug, att.PaneID, err)
			return
		}
		// Run errors can carry provider text: keys redacted, no control
		// characters in the log, the feed or a toast (as runFailed does).
		msg := textutil.SanitizeModelText(redactErr(err).Error())
		e.d.Logf("draft %s by %s on %s: %s", id, d.Slug, att.PaneID, msg)
		e.feed(att.PaneID, d.Slug, turn, state.OutcomeError, "draft failed: "+msg)
		tr.mark(turn, state.OutcomeError, "draft failed: "+msg)
		e.notify(d, "draft failed: "+msg)
	}
	// The metadata comes first, so a crash at any later point leaves a
	// record the next engine start cleans up. It is deleted last (this defer
	// runs after the ref and worktree cleanups below) unless it is kept as a
	// ready or rejected patch.
	// A ref that could not be dropped keeps its record (as drop-pending, or
	// as the rejected record it already is) so the next sweep retries.
	crashed, keepRecord, refStuck := false, false, false
	meta := patchstore.Meta{ID: id, Dot: d.Slug, Pane: att.PaneID, Repo: repo, Status: patchstore.StatusDrafting}
	if err := e.d.Patches.Put(meta); err != nil {
		fail(err)
		return
	}
	defer func() {
		if keepRecord || e.stopBefore(&crashed, "record") {
			return
		}
		if refStuck {
			if _, err := e.d.Patches.Update(id, func(m *patchstore.Meta) { m.Status = patchstore.StatusDropPending }); err != nil {
				e.d.Logf("draft %s: keep record for the ref retry: %v", id, err)
			}
			return
		}
		if err := e.d.Patches.Delete(id); err != nil {
			e.d.Logf("draft %s: delete record: %v", id, err)
		}
	}()
	// Registered before the snapshot: a cancellation landing mid-snapshot may
	// leave a ref, and dropping one that was never made is harmless.
	keepRef := false
	defer func() {
		if keepRef || e.stopBefore(&crashed, "ref") {
			return
		}
		if err := worktree.DropSnapshot(context.Background(), g, repo, id); err != nil {
			refStuck = true
			e.d.Logf("draft %s: drop snapshot ref (retried by the next sweep): %v", id, err)
		}
	}()
	s, err := worktree.TakeSnapshot(ctx, g, repo, id, maxSnapshotIndex)
	if err != nil {
		fail(err)
		return
	}
	meta.Snapshot, meta.Ref, meta.BaseHead = s.Commit, s.Ref, s.BaseHead
	if err := e.d.Patches.Put(meta); err != nil {
		fail(err)
		return
	}
	dir := filepath.Join(e.d.WorktreesDir, id)
	defer func() {
		if e.stopBefore(&crashed, "worktree") {
			return
		}
		if err := worktree.Remove(context.Background(), g, repo, dir); err != nil {
			e.d.Logf("draft %s: remove worktree: %v", id, err)
		}
	}()
	co, err := worktree.Add(ctx, g, s, dir)
	if err != nil {
		fail(err)
		return
	}
	e.setDraftPhase(id, view.DraftRunning)
	if e.projectDotGone(d) { // Phase 9: fresh trust, right before write tools start
		fail(errors.New("its project file changed or lost its trust: review it in the library"))
		return
	}
	be, err := e.d.Backend(d.Backend.Kind)
	if err != nil {
		fail(err)
		return
	}
	mem := e.loadMemory(&att, d, turns) // memory goes in the user prompt only
	req := requestFor(d, co.Dir)
	req.Turns, req.Question, req.Memory = turns, instruction, mem.block
	req.Pass, req.Write, req.RepoRead, req.Learn, req.Force = backend.PassWrite, true, true, false, false
	rctx, cancel := context.WithTimeout(ctx, d.Capabilities.WriteTimeout())
	started := time.Now()
	res, err := be.Run(rctx, req)
	tr.usage(res, time.Since(started))
	cancel()
	e.chargeDraft(id, att.PaneID, d.Slug, deltaOf(res)) // not for an abandoned draft or a closed pane (Phase 5)
	if err != nil {
		fail(err)
		return
	}
	if quiet() {
		fail(ErrPaneClosed)
		return
	}
	e.touchMemory(mem)
	e.setDraftPhase(id, view.DraftValidating)
	p, err := worktree.CaptureMax(ctx, g, co, s, d.Capabilities.MaxPatchBytes) // never buffers a huge draft whole
	if err != nil {
		fail(err)
		return
	}
	caps := worktree.Caps{MaxBytes: d.Capabilities.MaxPatchBytes, MaxFiles: d.Capabilities.MaxFiles, Deny: e.denyCheck()}
	if err := worktree.Validate(ctx, g, co, p, caps); err != nil {
		if errors.Is(err, worktree.ErrEmptyPatch) {
			if !quiet() {
				e.feed(att.PaneID, d.Slug, turn, state.OutcomePass, "draft produced no changes")
				tr.mark(turn, state.OutcomePass, "draft produced no changes")
			}
			return
		}
		if quiet() {
			fail(err)
			return
		}
		reason := textutil.SanitizeModelText(redactErr(err).Error()) // names paths the dot chose
		if _, err := e.d.Patches.Update(id, func(m *patchstore.Meta) { m.Status, m.Reason = patchstore.StatusRejected, reason }); err == nil {
			keepRecord = true
		}
		e.feed(att.PaneID, d.Slug, turn, state.OutcomeError, "patch rejected ("+reason+")")
		tr.mark(turn, state.OutcomeError, "patch rejected ("+reason+")")
		e.notify(d, "patch rejected: "+reason)
		return
	}
	// Meta.Summary and the card are redacted, and the text as written is
	// never stored: the record only says whether it held a secret, which is
	// what memory's outcome paths need (a redacted ledger line, no entry).
	raw := strings.TrimSpace(textutil.SanitizeModelText(res.Verdict.Message))
	summary := strings.TrimSpace(credentials.Redact(raw))
	found, _ := memory.ScanSecrets(raw)
	hadSecret := summary != raw || found
	if summary == "" {
		summary = "Drafted a fix."
	}
	severity := res.Verdict.Severity
	if severity != "warn" && severity != "critical" {
		severity = "info"
	}
	if quiet() {
		fail(ErrPaneClosed) // no card for a pane that is gone: nobody could apply it
		return
	}
	if toolloop.AgentConfigAny(p.Paths()) { // allowed, but loudly: first on the card
		p.Flags = append([]string{toolloop.FlagAgentConfig}, p.Flags...)
	}
	meta.Summary, meta.Stat, meta.Files, meta.Paths, meta.Flags, meta.Status = summary, p.Stat, p.Files, p.Paths(), p.Flags, patchstore.StatusReady
	meta.SummaryHadSecret = hadSecret
	// Only what passed Validate is stored, and the engine applies exactly
	// those bytes (hash-verified by patchstore.Diff).
	if err := e.d.Patches.PutPatch(meta, p.Diff); err != nil {
		fail(err)
		return
	}
	c, err := e.d.Queue.Add(queue.Card{Kind: queue.KindPatch, Dot: d.Slug, DotName: d.Name, Glyph: d.Glyph, PaneID: att.PaneID,
		Turn: turn, Severity: severity, Body: summary, Mode: string(dots.ModeQueue), Project: att.ProjectID,
		PatchID: id, DiffStat: p.Stat, PatchFlags: p.Flags})
	if err != nil {
		fail(err)
		return
	}
	if quiet() {
		// The pane closed between the check and the card: take the card back.
		_ = e.d.Queue.SetStatus(c.ID, queue.StatusDismissed)
		fail(ErrPaneClosed)
		return
	}
	e.hook("draft-carded")
	if _, err := e.d.Patches.Update(id, func(m *patchstore.Meta) { m.Card = c.ID }); err != nil {
		// Take the card back (under dmu, like any dismiss) and let the
		// cleanups delete the patch, its record and its ref: a card its
		// record doesn't name can't be trusted to apply or discard cleanly.
		e.dmu.Lock()
		_ = e.d.Queue.SetStatus(c.ID, queue.StatusDismissed)
		e.dmu.Unlock()
		fail(fmt.Errorf("record the patch card: %w", err))
		return
	}
	keepRef, keepRecord = true, true
	e.feed(att.PaneID, d.Slug, turn, state.OutcomeDrafted, p.Stat)
	tr.mark(turn, state.OutcomeDrafted, summary) // the redacted summary on the card, never the raw model text
	tr.card(c)
	e.reportPending(att.PaneID)
	e.broadcast()
}

// patchCard returns a patch card by id.
func (e *Engine) patchCard(id string) (queue.Card, error) {
	c, ok := e.d.Queue.Get(id)
	if !ok {
		return c, fmt.Errorf("card %s: %w", id, queue.ErrNotFound)
	}
	if c.Kind != queue.KindPatch || c.PatchID == "" {
		return c, fmt.Errorf("card %s: %w", id, ErrNotPatch)
	}
	if e.d.Patches == nil {
		return c, errors.New("drafts are unavailable: the engine has no patch store")
	}
	return c, nil
}

// ErrUnreviewed refuses an apply that does not name the patch it reviewed.
var ErrUnreviewed = errors.New("open the patch to review it before applying")

// ApplyPatch applies a patch card to the working tree (gate 2, confirmed in
// the diff viewer): only while the agent is idle, only into the repository
// the patch was drafted for, never into the index, never as a commit.
// Conflicts leave markers in the files and are reported, not an error.
// wantSHA256 is the hash of the patch the viewer showed (P7-R7): it is
// required, and must equal the stored patch's hash, or nothing is applied.
func (e *Engine) ApplyPatch(cardID, wantSHA256 string) error {
	if strings.TrimSpace(wantSHA256) == "" {
		return ErrUnreviewed
	}
	if _, err := e.patchCard(cardID); err != nil {
		return err
	}
	e.hook("apply-before-reserve")
	// Reserve and re-read the status under dmu (no git here): a discard that
	// finished meanwhile is seen, and a SendCards that started later is
	// refused by the reservation, one that finished earlier left the card
	// sent. dmu is released before any git runs.
	e.dmu.Lock()
	release, err := e.reserveCard(cardID)
	var c queue.Card
	if err == nil {
		c, err = e.patchCard(cardID)
		if err == nil && c.Status != queue.StatusPending {
			err = fmt.Errorf("card %s is %s: only a pending patch can be applied", cardID, c.Status)
		}
		if err != nil {
			release()
		}
	}
	e.dmu.Unlock()
	if err != nil {
		return err
	}
	defer release()
	e.hook("apply-reserved")
	// Read and verify the patch: these exact bytes are what gets applied.
	diff, m, err := e.d.Patches.Diff(c.PatchID)
	if err != nil {
		return err
	}
	if m.Status != patchstore.StatusReady {
		return fmt.Errorf("patch %s is %s", c.PatchID, m.Status)
	}
	if !strings.EqualFold(wantSHA256, m.SHA256) {
		return errors.New("the patch changed since you reviewed it; open it again")
	}
	p, err := e.d.Herdr.PaneGet(e.ctx, c.PaneID)
	if err != nil {
		return fmt.Errorf("pane %s: %w", c.PaneID, err)
	}
	if delivery.CanDeliver(p, false) != delivery.Now {
		return ErrAgentBusy // the agent could be editing the same files
	}
	top, err := worktree.RepoRoot(e.ctx, e.d.Git, p.WorkDir())
	if err != nil || !sameRepo(top, m.Repo) {
		return fmt.Errorf("the pane works in %s now, but the patch is for %s", p.WorkDir(), m.Repo)
	}
	// The plain Git (the user's own filters), the repository top-level, and
	// exactly the verified bytes.
	conflicts, err := worktree.Apply(e.ctx, e.d.Git, m.Repo, diff)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	e.hook("apply-applied")
	note, detail := "applied to your working tree (not staged)", m.Stat
	var flags []string
	if len(conflicts) > 0 {
		note = fmt.Sprintf("applied with conflicts in %d %s: %s", len(conflicts), fileWord(len(conflicts)), strings.Join(conflicts, ", "))
		detail, flags = note, []string{queue.FlagConflicts}
	}
	if err := e.d.Queue.SetStatusNote(cardID, queue.StatusApplied, note, flags...); err != nil {
		e.d.Logf("card %s: %v", cardID, err)
	}
	if _, err := e.d.Patches.Update(c.PatchID, func(m *patchstore.Meta) { m.Status, m.Conflicts = patchstore.StatusApplied, conflicts }); err != nil {
		e.d.Logf("patch %s: %v", c.PatchID, err)
	}
	if err := worktree.DropSnapshot(e.ctx, e.d.Git, m.Repo, c.PatchID); err != nil {
		e.d.Logf("patch %s: drop snapshot ref: %v", c.PatchID, err)
	}
	e.feed(c.PaneID, c.Dot, c.Turn, state.OutcomeApplied, detail)
	if len(conflicts) > 0 {
		e.notify(dots.Dot{Name: c.DotName, Glyph: c.Glyph}, note)
	}
	oc := c
	if m.Summary != "" {
		oc.Body = m.Summary
	}
	e.spawnOutcomeOf(oc, memory.OutcomeApplied, m.SummaryHadSecret)
	e.reportPending(c.PaneID)
	e.broadcast()
	return nil
}

// DiscardPatch throws a patch card away: the card is dismissed, and its
// patch, metadata and snapshot ref are deleted. Memory gets a ledger line.
func (e *Engine) DiscardPatch(cardID string) error {
	if _, err := e.patchCard(cardID); err != nil {
		return err
	}
	// The same per-card reservation as ApplyPatch, for the whole discard.
	release, err := e.reserveCard(cardID)
	if err != nil {
		return err
	}
	defer release()
	c, err := e.patchCard(cardID)
	if err != nil {
		return err
	}
	e.hook("discard-reserved")
	// A patch already applied (an apply interrupted after the patch landed,
	// before the card was marked) is never discarded: memory would record
	// "discarded" after "applied". The reservation keeps an apply out.
	// The card is marked applied instead (D8), so it doesn't stay on screen
	// with no way to dismiss it.
	m, merr := e.d.Patches.Get(c.PatchID)
	if merr == nil && m.Status == patchstore.StatusApplied {
		if c.Status == queue.StatusPending || c.Status == queue.StatusOrphaned {
			e.dmu.Lock()
			err := e.d.Queue.SetStatusNote(cardID, queue.StatusApplied, "applied to your working tree (not staged)")
			e.dmu.Unlock()
			if err != nil {
				e.d.Logf("card %s: %v", cardID, err)
			}
			e.reportPending(c.PaneID)
			e.broadcast()
		}
		return fmt.Errorf("patch %s was already applied to your working tree", c.PatchID)
	}
	// The status changes under dmu, like Dismiss of any card, so it never
	// interleaves with a delivery; the git and patch-file work comes after.
	e.dmu.Lock()
	err = e.d.Queue.SetStatus(cardID, queue.StatusDismissed)
	e.dmu.Unlock()
	if err != nil {
		return err
	}
	oc := c
	stuck := false
	if merr == nil {
		if m.Summary != "" {
			oc.Body = m.Summary
		}
		if m.Repo != "" {
			if err := worktree.DropSnapshot(e.ctx, e.d.Git, m.Repo, c.PatchID); err != nil {
				stuck = true
				e.d.Logf("patch %s: drop snapshot ref (retried by the next sweep): %v", c.PatchID, err)
			}
		}
	}
	if stuck { // the record stays so the sweep can find and drop the ref
		if _, err := e.d.Patches.Update(c.PatchID, func(m *patchstore.Meta) { m.Status = patchstore.StatusDropPending }); err != nil {
			e.d.Logf("patch %s: %v", c.PatchID, err)
		}
	} else if err := e.d.Patches.Delete(c.PatchID); err != nil {
		e.d.Logf("patch %s: %v", c.PatchID, err)
	}
	e.spawnOutcomeOf(oc, memory.OutcomeDiscarded, merr == nil && m.SummaryHadSecret)
	e.reportPending(c.PaneID)
	e.broadcast()
	return nil
}

// cardReserved reports whether a patch card is being applied or discarded.
func (e *Engine) cardReserved(cardID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.applying[cardID]
}

// reserveCard marks a patch card as being applied or discarded; the other
// operation is refused until release.
func (e *Engine) reserveCard(cardID string) (release func(), err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.applying[cardID] {
		return nil, fmt.Errorf("card %s is already being applied or discarded", cardID)
	}
	e.applying[cardID] = true
	return func() {
		e.mu.Lock()
		delete(e.applying, cardID)
		e.mu.Unlock()
	}, nil
}

func (e *Engine) hook(point string) {
	if e.testHook != nil {
		e.testHook(point)
	}
}

// patchMessage is what "send to agent" delivers for a patch card: where the
// patch is and how to review and apply it, never the diff itself.
func (e *Engine) patchMessage(c queue.Card) string {
	// Plain double quotes, no escapes: a Windows path keeps its single
	// backslashes, and bash, zsh, PowerShell and cmd all read it the same.
	path := `"` + e.d.Patches.PatchPath(c.PatchID) + `"`
	return fmt.Sprintf("Patch ready: %s. Review with git apply --stat %s, apply with git apply %s.", c.DiffStat, path, path)
}

// forDelivery swaps each patch card's body for its patch message.
func (e *Engine) forDelivery(cards []queue.Card) []queue.Card {
	out := append([]queue.Card(nil), cards...)
	for i, c := range out {
		if c.Kind == queue.KindPatch && e.d.Patches != nil {
			out[i].Body = e.patchMessage(c)
		}
	}
	return out
}

// patchSent marks a delivered patch card's patch as sent (kept 7 days) and
// records the usual "sent" outcome. It runs in the background: no patch I/O
// under dmu. Memory gets the redacted summary and whether it held a secret
// (see runDraft).
func (e *Engine) patchSent(c queue.Card) {
	if c.Kind != queue.KindPatch || e.d.Patches == nil {
		return
	}
	e.spawn(func() {
		oc := c
		m, err := e.d.Patches.Update(c.PatchID, func(m *patchstore.Meta) { m.Status = patchstore.StatusSent })
		if err != nil {
			e.d.Logf("patch %s: %v", c.PatchID, err)
		} else if m.Summary != "" {
			oc.Body = m.Summary
		}
		if e.memEnabled() {
			e.recordOutcome(oc, memory.OutcomeSent, "", false, err == nil && m.SummaryHadSecret)
		}
	})
}

// draftViews returns the pane's running drafts, oldest first. Caller holds e.mu.
func (e *Engine) draftViews(paneID string) []view.DraftState {
	var out []view.DraftState
	for id, dr := range e.drafts {
		if dr.pane == paneID && !dr.abandoned {
			out = append(out, view.DraftState{ID: id, Slug: dr.slug, Phase: dr.phase, StartedAt: dr.started})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// sameRepo reports whether a and b are the same directory: by file identity
// (symlinks, case variants and Windows short names all match), or, when
// either can't be stat'ed, by the cleaned, symlink-resolved paths (compared
// case-insensitively on Windows, see pathsEqual).
func sameRepo(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	if aerr == nil && berr == nil {
		return os.SameFile(ai, bi)
	}
	if aerr == nil || berr == nil {
		return false // one exists and the other doesn't
	}
	clean := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		return filepath.Clean(p)
	}
	return pathsEqual(clean(a), clean(b))
}

func fileWord(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}
