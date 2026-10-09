package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/delivery"
	"herdr-dots/internal/dots"
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
}

// Draft starts a write-capable dot on an isolated copy of the pane's
// repository (gate 1, confirmed in the Dots pane). instruction "" with a
// fromCardID drafts that card's advice. It returns the draft id at once;
// the draft runs in the background and ends as a patch card, a feed entry
// or a toast.
func (e *Engine) Draft(ctx context.Context, paneID, slug, instruction, fromCardID string) (string, error) {
	cfg := e.config()
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
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return "", fmt.Errorf("%w: %s", state.ErrNoAttachment, paneID)
	}
	if att.Find(slug) == nil {
		return "", fmt.Errorf("dot %q is not attached to %s", slug, paneID)
	}
	e.mu.Lock()
	d, ok := e.dots[slug]
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
	for _, dr := range e.drafts {
		if dr.slug == slug {
			e.mu.Unlock()
			return "", ErrDraftRunning
		}
	}
	if len(e.drafts) >= max(1, cfg.MaxConcurrentDrafts) {
		e.mu.Unlock()
		return "", ErrDraftLimit
	}
	// Checked again with the draft about to be reserved: asks and drafts
	// that started meanwhile count as calls already made.
	if err := e.draftBudgetLocked(paneID, d); err != nil {
		e.mu.Unlock()
		return "", err
	}
	e.drafts[id] = &draftRun{pane: paneID, slug: slug, phase: view.DraftSnapshot, started: time.Now()}
	e.mu.Unlock()
	e.broadcast()
	e.spawn(func() { e.runDraft(id, att, repo, d, instruction, turns) })
	return id, nil
}

// draftBudgetLocked refuses a draft when the dot's or the pane's budget is
// spent: a draft counts as one call (and its cost) like any run, and, as for
// MCP asks (askBlockedLocked), asks and drafts in flight on the pane count as
// calls already made. Caller holds e.mu (e.mu -> stores).
func (e *Engine) draftBudgetLocked(paneID string, d dots.Dot) error {
	cfg := e.config()
	inFlight := 0
	for k := range e.asking {
		if k.pane == paneID {
			inFlight++
		}
	}
	for _, dr := range e.drafts {
		if dr.pane == paneID {
			inFlight++
		}
	}
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
// list plus the configured tool_deny_globs, for worktree.Validate.
func (e *Engine) denyCheck() func(rel string) bool {
	deny := append(append([]string(nil), toolloop.DefaultDeny...), e.config().ToolDenyGlobs...)
	return func(rel string) bool { return toolloop.Denied(rel, deny) }
}

// runDraft snapshots, checks out, runs the dot with write tools in the
// worktree, captures and validates the diff, and stores it as a patch card.
// The worktree is always removed; the snapshot ref stays only behind a card.
func (e *Engine) runDraft(id string, att state.Attachment, repo string, d dots.Dot, instruction string, turns []transcript.Turn) {
	defer func() {
		e.mu.Lock()
		delete(e.drafts, id)
		e.mu.Unlock()
		e.broadcast()
	}()
	ctx, g := e.ctx, e.d.Git
	turn := 0
	if len(turns) > 0 {
		turn = turns[len(turns)-1].Index
	}
	fail := func(err error) {
		_ = e.d.Patches.Delete(id)
		if e.ctx.Err() != nil || errors.Is(err, ErrPaneClosed) {
			// The engine is stopping or the pane is gone: nobody to tell.
			e.d.Logf("draft %s by %s on %s ended: %v", id, d.Slug, att.PaneID, err)
			return
		}
		// Run errors can carry provider text: keys redacted, no control
		// characters in the log, the feed or a toast (as runFailed does).
		msg := textutil.SanitizeModelText(redactErr(err).Error())
		e.d.Logf("draft %s by %s on %s: %s", id, d.Slug, att.PaneID, msg)
		e.feed(att.PaneID, d.Slug, turn, state.OutcomeError, "draft failed: "+msg)
		e.notify(d, "draft failed: "+msg)
	}
	// The metadata comes first, so a crash at any later point leaves a
	// record the next engine start cleans up.
	meta := patchstore.Meta{ID: id, Dot: d.Slug, Pane: att.PaneID, Repo: repo, Status: patchstore.StatusDrafting}
	if err := e.d.Patches.Put(meta); err != nil {
		fail(err)
		return
	}
	s, err := worktree.TakeSnapshot(ctx, g, repo, id, maxSnapshotIndex)
	if err != nil {
		fail(err)
		return
	}
	keepRef := false
	defer func() {
		if !keepRef {
			if err := worktree.DropSnapshot(context.Background(), g, repo, id); err != nil {
				e.d.Logf("draft %s: drop snapshot ref: %v", id, err)
			}
		}
	}()
	meta.Snapshot, meta.Ref, meta.BaseHead = s.Commit, s.Ref, s.BaseHead
	if err := e.d.Patches.Put(meta); err != nil {
		fail(err)
		return
	}
	dir := filepath.Join(e.d.WorktreesDir, id)
	defer func() {
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
	res, err := be.Run(rctx, req)
	cancel()
	e.charge(att.PaneID, d.Slug, deltaOf(res)) // not if the pane closed meanwhile (Phase 5)
	if err != nil {
		fail(err)
		return
	}
	e.touchMemory(mem)
	e.setDraftPhase(id, view.DraftValidating)
	p, err := worktree.Capture(ctx, g, co, s)
	if err != nil {
		fail(err)
		return
	}
	caps := worktree.Caps{MaxBytes: d.Capabilities.MaxPatchBytes, MaxFiles: d.Capabilities.MaxFiles, Deny: e.denyCheck()}
	if err := worktree.Validate(ctx, g, co, p, caps); err != nil {
		if errors.Is(err, worktree.ErrEmptyPatch) {
			_ = e.d.Patches.Delete(id)
			e.feed(att.PaneID, d.Slug, turn, state.OutcomePass, "draft produced no changes")
			return
		}
		if e.ctx.Err() != nil {
			fail(err)
			return
		}
		reason := textutil.SanitizeModelText(redactErr(err).Error()) // names paths the dot chose
		_, _ = e.d.Patches.Update(id, func(m *patchstore.Meta) { m.Status, m.Reason = patchstore.StatusRejected, reason })
		e.feed(att.PaneID, d.Slug, turn, state.OutcomeError, "patch rejected ("+reason+")")
		e.notify(d, "patch rejected: "+reason)
		return
	}
	summary := strings.TrimSpace(credentials.Redact(textutil.SanitizeModelText(res.Verdict.Message)))
	if summary == "" {
		summary = "Drafted a fix."
	}
	severity := res.Verdict.Severity
	if severity != "warn" && severity != "critical" {
		severity = "info"
	}
	if _, ok := e.d.Attachments.Get(att.PaneID); !ok {
		fail(ErrPaneClosed) // no card for a pane that is gone: nobody could apply it
		return
	}
	meta.Summary, meta.Stat, meta.Files, meta.Paths, meta.Flags, meta.Status = summary, p.Stat, p.Files, p.Paths(), p.Flags, patchstore.StatusReady
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
	if _, ok := e.d.Attachments.Get(att.PaneID); !ok {
		// The pane closed between the check and the card: take the card back.
		_ = e.d.Queue.SetStatus(c.ID, queue.StatusDismissed)
		fail(ErrPaneClosed)
		return
	}
	keepRef = true
	_, _ = e.d.Patches.Update(id, func(m *patchstore.Meta) { m.Card = c.ID })
	e.feed(att.PaneID, d.Slug, turn, state.OutcomeDrafted, p.Stat)
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

// ApplyPatch applies a patch card to the working tree (gate 2, confirmed in
// the diff viewer): only while the agent is idle, only into the repository
// the patch was drafted for, never into the index, never as a commit.
// Conflicts leave markers in the files and are reported, not an error.
func (e *Engine) ApplyPatch(cardID string) error { return e.ApplyPatchChecked(cardID, "") }

// ApplyPatchChecked is ApplyPatch for a caller that shows the patch first
// (the diff viewer): a non-empty wantSHA256 must equal the stored patch's
// hash, or nothing is applied (the patch changed since it was reviewed).
func (e *Engine) ApplyPatchChecked(cardID, wantSHA256 string) error {
	c, err := e.patchCard(cardID)
	if err != nil {
		return err
	}
	if c.Status != queue.StatusPending {
		return fmt.Errorf("card %s is %s: only a pending patch can be applied", cardID, c.Status)
	}
	e.mu.Lock()
	if e.applying[cardID] {
		e.mu.Unlock()
		return fmt.Errorf("card %s is already being applied", cardID)
	}
	e.applying[cardID] = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.applying, cardID)
		e.mu.Unlock()
	}()
	// Read and verify the patch: these exact bytes are what gets applied.
	diff, m, err := e.d.Patches.Diff(c.PatchID)
	if err != nil {
		return err
	}
	if m.Status != patchstore.StatusReady {
		return fmt.Errorf("patch %s is %s", c.PatchID, m.Status)
	}
	if wantSHA256 != "" && !strings.EqualFold(wantSHA256, m.SHA256) {
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
	e.spawnOutcome(c, memory.OutcomeApplied)
	e.reportPending(c.PaneID)
	e.broadcast()
	return nil
}

// DiscardPatch throws a patch card away: the card is dismissed, and its
// patch, metadata and snapshot ref are deleted. Memory gets a ledger line.
func (e *Engine) DiscardPatch(cardID string) error {
	c, err := e.patchCard(cardID)
	if err != nil {
		return err
	}
	e.mu.Lock()
	busy := e.applying[cardID]
	e.mu.Unlock()
	if busy {
		return fmt.Errorf("card %s is being applied", cardID)
	}
	if err := e.d.Queue.SetStatus(cardID, queue.StatusDismissed); err != nil {
		return err
	}
	if m, err := e.d.Patches.Get(c.PatchID); err == nil && m.Repo != "" {
		if err := worktree.DropSnapshot(e.ctx, e.d.Git, m.Repo, c.PatchID); err != nil {
			e.d.Logf("patch %s: drop snapshot ref: %v", c.PatchID, err)
		}
	}
	if err := e.d.Patches.Delete(c.PatchID); err != nil {
		e.d.Logf("patch %s: %v", c.PatchID, err)
	}
	e.spawnOutcome(c, memory.OutcomeDiscarded)
	e.reportPending(c.PaneID)
	e.broadcast()
	return nil
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

// patchSent marks a delivered patch card's patch as sent (kept 7 days).
func (e *Engine) patchSent(c queue.Card) {
	if c.Kind != queue.KindPatch || e.d.Patches == nil {
		return
	}
	e.spawn(func() {
		if _, err := e.d.Patches.Update(c.PatchID, func(m *patchstore.Meta) { m.Status = patchstore.StatusSent }); err != nil {
			e.d.Logf("patch %s: %v", c.PatchID, err)
		}
	})
}

// draftViews returns the pane's running drafts, oldest first. Caller holds e.mu.
func (e *Engine) draftViews(paneID string) []view.DraftState {
	var out []view.DraftState
	for id, dr := range e.drafts {
		if dr.pane == paneID {
			out = append(out, view.DraftState{ID: id, Slug: dr.slug, Phase: dr.phase, StartedAt: dr.started})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

func sameRepo(a, b string) bool {
	clean := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		return filepath.Clean(p)
	}
	return clean(a) == clean(b)
}

func fileWord(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}
