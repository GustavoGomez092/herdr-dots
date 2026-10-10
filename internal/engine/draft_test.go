package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
	"herdr-dots/internal/worktree"
)

// drafter is a backend whose draft runs edit files in Request.Cwd like a
// write-capable dot would; review runs always pass.
type drafter struct {
	mu   sync.Mutex
	edit func(dir string)
	msg  string
	err  error
	gate chan struct{} // optional: draft runs block until closed
	deaf bool          // a backend that ignores its context while gated
	reqs []backend.Request
}

func (f *drafter) Kind() string                { return "claude-cli" }
func (f *drafter) Check(context.Context) error { return nil }
func (f *drafter) Run(ctx context.Context, r backend.Request) (backend.Result, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	edit, msg, err, gate := f.edit, f.msg, f.err, f.gate
	f.mu.Unlock()
	if !backend.WriteMode(r) {
		return backend.Result{Verdict: backend.Verdict{Severity: "info"}}, nil
	}
	if gate != nil {
		if f.deaf {
			<-gate
		} else {
			select {
			case <-gate:
			case <-ctx.Done():
				return backend.Result{}, ctx.Err()
			}
		}
	}
	if err != nil {
		return backend.Result{InputTokens: 7}, err
	}
	if edit != nil {
		edit(r.Cwd)
	}
	return backend.Result{Verdict: backend.Verdict{Relevant: true, Severity: "warn", Message: msg}, InputTokens: 100, OutputTokens: 20}, nil
}

func (f *drafter) writes() []backend.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []backend.Request
	for _, r := range f.reqs {
		if backend.WriteMode(r) {
			out = append(out, r)
		}
	}
	return out
}

func (f *drafter) set(edit func(string), err error) {
	f.mu.Lock()
	f.edit, f.err = edit, err
	f.mu.Unlock()
}

// gitTestEnv isolates git from the developer's own configuration.
func gitTestEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "HOME" || k == "USERPROFILE" || k == "XDG_CONFIG_HOME" || strings.HasPrefix(k, "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "GIT_CONFIG_NOSYSTEM=1")
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(t, dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// gitTry is gitT for a wait condition that polls while the engine runs git
// in the same repository: `git worktree list` fails while a concurrent
// `git worktree prune` removes .git/worktrees ("Invalid path"), which only
// means "not yet".
func gitTry(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false",
		"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Env = gitTestEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func writeT(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// draftRepo is a repository with one commit: auth.js (three lines).
func draftRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitT(t, repo, "init", "-q")
	writeT(t, filepath.Join(repo, "auth.js"), "const a = 1\nlocalStorage.setItem('jwt', token)\nconst c = 3\n")
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-qm", "init")
	return repo
}

// fixJWT is what the drafting dot does in its worktree.
func fixJWT(dir string) {
	_ = os.WriteFile(filepath.Join(dir, "auth.js"), []byte("const a = 1\nsetCookie('jwt', token, {httpOnly: true})\nconst c = 3\n"), 0o644)
}

func writeDot(name string) dots.Dot {
	d := dot(name, dots.ModeQueue)
	d.Capabilities.Write = true
	return d
}

type draftOpts struct {
	cfg    func(*config.Config)
	before func(p *patchstore.Store, wts string, g worktree.Git, repo string) // runs before the engine starts
	deps   func(d *Deps)                                                      // adjusts the engine Deps (the queue is real)
}

type draftRig struct {
	*rig
	dr      *drafter
	repo    string
	patches *patchstore.Store
	wts     string
	g       worktree.Git
	mem     *memory.Store
}

func newDraftRig(t *testing.T, o draftOpts, ds ...dots.Dot) *draftRig {
	t.Helper()
	env := gitTestEnv(t)
	dir := t.TempDir()
	dr := &draftRig{dr: &drafter{msg: "Moved the JWT to an httpOnly cookie. SameSite=Lax."}, repo: draftRepo(t),
		patches: patchstore.Open(filepath.Join(dir, "patches")), wts: filepath.Join(dir, "worktrees"),
		g: worktree.Git{Environ: func() []string { return env }}, mem: memory.Open(filepath.Join(dir, "memory"), memory.DefaultLimits())}
	if o.before != nil {
		o.before(dr.patches, dr.wts, dr.g, dr.repo)
	}
	dr.rig = newRigWith(t, func(d *Deps) {
		d.Backend = func(string) (backend.Backend, error) { return dr.dr, nil }
		d.Config.WriteEnabled = true
		if o.cfg != nil {
			o.cfg(&d.Config)
		}
		d.Patches, d.WorktreesDir, d.Git = dr.patches, dr.wts, dr.g
		d.Memory = dr.mem
		if o.deps != nil {
			o.deps(d)
		}
		d.ResolveProject = func(string) (memory.Project, error) { return memory.Project{ID: testProject, Name: "repo"}, nil }
	}, ds...)
	dr.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: dr.repo,
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	dr.readers["s1"].add(transcript.Turn{User: "store the token", Assistant: "saved it in localStorage"})
	return dr
}

func (r *draftRig) patchCard(t *testing.T) queue.Card {
	t.Helper()
	var c queue.Card
	eventually(t, "a patch card", func() bool {
		for _, x := range r.q.Pending(pane) {
			if x.Kind == queue.KindPatch {
				c = x
				return true
			}
		}
		return false
	})
	return c
}

func (r *draftRig) draftsDone(t *testing.T) {
	t.Helper()
	eventually(t, "no draft running", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Drafts) == 0
	})
}

// reviewsDone waits until the n turn reviews attaching started have been
// charged and no dot is thinking.
func (r *draftRig) reviewsDone(t *testing.T, n int) {
	t.Helper()
	eventually(t, "attach reviews done", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		for _, d := range p.Dots {
			if d.Status == view.StatusThinking {
				return false
			}
		}
		return r.usage.PaneCalls(pane) >= n
	})
}

func (r *draftRig) refs(t *testing.T) string {
	t.Helper()
	return gitT(t, r.repo, "for-each-ref", "--format=%(refname)", worktree.RefPrefix)
}

func (r *draftRig) feedHasDraft(outcome, detail string) bool {
	for _, f := range r.e.d.Feed.Recent(pane, 50) {
		if f.Outcome == outcome && strings.Contains(f.Detail, detail) {
			return true
		}
	}
	return false
}

func TestDraftHappyPathMakesAPatchCard(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.mem.Add(testProject, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "Tokens live in httpOnly cookies here."}); err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(r.repo, "notes.md"), "the user's own uncommitted note\n")
	index, _ := os.ReadFile(filepath.Join(r.repo, ".git", "index"))
	id, err := r.e.Draft(context.Background(), pane, "security", "Move the JWT out of localStorage.", "")
	if err != nil || !worktree.ValidID(id) {
		t.Fatalf("Draft = %q, %v", id, err)
	}
	c := r.patchCard(t)
	r.draftsDone(t)
	if c.PatchID != id || c.DiffStat != "1 file +1 −1" || c.Body != "Moved the JWT to an httpOnly cookie. SameSite=Lax." || c.Mode != "queue" || c.Status != queue.StatusPending {
		t.Fatalf("card %+v", c)
	}
	reqs := r.dr.writes()
	if len(reqs) != 1 {
		t.Fatalf("%d draft runs", len(reqs))
	}
	req := reqs[0]
	if req.Question != "Move the JWT out of localStorage." || req.Learn || !strings.HasPrefix(req.Cwd, r.wts) || !strings.Contains(req.Memory, "Tokens live in httpOnly cookies here.") {
		t.Fatalf("request %+v", req)
	}
	if strings.Contains(backend.SystemPrompt(req), "httpOnly cookies here") {
		t.Fatal("memory must never be in the system prompt")
	}
	if _, err := os.Stat(req.Cwd); !os.IsNotExist(err) {
		t.Fatal("the worktree must be removed once the diff is captured")
	}
	if wl := gitT(t, r.repo, "worktree", "list", "--porcelain"); strings.Count(wl, "worktree ") != 1 {
		t.Fatalf("git still lists a draft worktree:\n%s", wl)
	}
	if got, _ := os.ReadFile(filepath.Join(r.repo, "auth.js")); strings.Contains(string(got), "setCookie") {
		t.Fatal("a draft must never touch the user's files")
	}
	if after, _ := os.ReadFile(filepath.Join(r.repo, ".git", "index")); string(after) != string(index) {
		t.Fatal("a draft must never touch the user's index")
	}
	if refs := r.refs(t); refs != worktree.RefPrefix+id {
		t.Fatalf("snapshot refs %q: the ref stays until the patch is resolved", refs)
	}
	m, err := r.patches.Get(id)
	if err != nil || m.Status != patchstore.StatusReady || m.Card != c.ID || !slices.Equal(m.Paths, []string{"auth.js"}) {
		t.Fatalf("meta %+v %v", m, err)
	}
	if !r.feedHasDraft(state.OutcomeDrafted, "1 file +1 −1") {
		t.Fatal("feed lacks the drafted entry")
	}
	if n := r.usage.Calls(pane, "security"); n < 1 {
		t.Fatalf("a draft counts as a call, got %d", n)
	}
}

func TestDraftNeedsBothOptInsAndAShellFreeBackend(t *testing.T) {
	codex := writeDot("Fixer")
	codex.Backend.Kind = "codex-cli"
	r := newDraftRig(t, draftOpts{cfg: func(c *config.Config) { c.WriteEnabled = false }}, writeDot("Security"), dot("Style", dots.ModeQueue), codex)
	r.attach(t, "security", "style", "fixer")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); !errors.Is(err, ErrWriteDisabled) {
		t.Fatalf("write_enabled off: %v", err)
	}
	cfg := r.e.config()
	cfg.WriteEnabled = true
	r.e.cfg.Store(&cfg)
	if _, err := r.e.Draft(context.Background(), pane, "style", "fix", ""); !errors.Is(err, ErrNotWriteCapable) {
		t.Fatalf("a dot without write: %v", err)
	}
	if _, err := r.e.Draft(context.Background(), pane, "fixer", "fix", ""); !errors.Is(err, ErrNotWriteCapable) {
		t.Fatalf("a codex dot: %v", err)
	}
	if len(r.dr.writes()) != 0 {
		t.Fatal("a refused draft must not run")
	}
	if p, _ := r.e.Snapshot().Pane(pane); len(p.Drafts) != 0 {
		t.Fatal("a refused draft must not show as running")
	}
}

func TestDraftRefusesOutsideGitAndWithoutAnInstruction(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "  ", ""); err == nil || !strings.Contains(err.Error(), "say what to fix") {
		t.Fatalf("empty instruction: %v", err)
	}
	if _, err := r.e.Draft(context.Background(), pane, "security", "", "999"); !errors.Is(err, queue.ErrNotFound) {
		t.Fatalf("unknown card: %v", err)
	}
	_ = r.atts.Update(pane, func(a *state.Attachment) { a.Cwd = t.TempDir() })
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); !errors.Is(err, worktree.ErrNotGit) {
		t.Fatalf("not a repo: %v", err)
	}
}

func TestDraftLimitsAndProgressInTheSnapshot(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"), writeDot("Backend"), writeDot("Docs"))
	gate := make(chan struct{})
	r.dr.mu.Lock()
	r.dr.gate, r.dr.edit = gate, fixJWT
	r.dr.mu.Unlock()
	r.attach(t, "security", "backend", "docs")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.Draft(context.Background(), pane, "security", "again", ""); !errors.Is(err, ErrDraftRunning) {
		t.Fatalf("same dot twice: %v", err)
	}
	if _, err := r.e.Draft(context.Background(), pane, "backend", "fix", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.Draft(context.Background(), pane, "docs", "fix", ""); !errors.Is(err, ErrDraftLimit) {
		t.Fatalf("a third draft: %v", err)
	}
	eventually(t, "both drafts running", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Drafts) == 2 && p.Drafts[0].Phase == view.DraftRunning && p.Drafts[1].Phase == view.DraftRunning
	})
	s := r.e.Snapshot()
	if !s.WriteEnabled || len(s.Library) != 3 || !s.Library[0].Write || s.Library[0].WriteTimeoutSeconds != 600 {
		t.Fatalf("snapshot %+v", s.Library)
	}
	close(gate)
	r.draftsDone(t)
	eventually(t, "two patch cards", func() bool { return len(r.q.Pending(pane)) == 2 })
}

func TestApplyPatchWaitsForAnIdleAgentAndTouchesOnlyTheWorkingTree(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	writeT(t, filepath.Join(r.repo, "staged.txt"), "staged by the user\n")
	gitT(t, r.repo, "add", "staged.txt")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusWorking, Cwd: r.repo, AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	if err := r.applyReviewed(c.ID); !errors.Is(err, ErrAgentBusy) {
		t.Fatalf("apply while working: %v", err)
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusPending {
		t.Fatal("a refused apply leaves the card pending")
	}
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: r.repo, AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	index, _ := os.ReadFile(filepath.Join(r.repo, ".git", "index"))
	head := gitT(t, r.repo, "rev-parse", "HEAD")
	if err := r.applyReviewed(c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.repo, "auth.js")); !strings.Contains(string(got), "setCookie") {
		t.Fatalf("auth.js = %q", got)
	}
	if after, _ := os.ReadFile(filepath.Join(r.repo, ".git", "index")); string(after) != string(index) || gitT(t, r.repo, "rev-parse", "HEAD") != head {
		t.Fatal("apply must never stage or commit")
	}
	if got, _ := r.q.Get(c.ID); got.Status != queue.StatusApplied || len(r.q.Visible(pane)) != 0 {
		t.Fatalf("card %+v", got)
	}
	if r.refs(t) != "" {
		t.Fatal("the snapshot ref goes once the patch is applied")
	}
	if m, _ := r.patches.Get(c.PatchID); m.Status != patchstore.StatusApplied {
		t.Fatalf("meta %+v", m)
	}
	if !r.feedHasDraft(state.OutcomeApplied, "1 file +1 −1") {
		t.Fatal("feed lacks the applied entry")
	}
	eventually(t, "an applied finding in memory", func() bool {
		es, _ := r.mem.List(testProject, memory.Filter{Scope: memory.ScopeDot, Dot: "security"})
		return len(es) == 1 && es[0].Text == "Applied patch: Moved the JWT to an httpOnly cookie." && es[0].Source == memory.SourceOutcome
	})
	if err := r.applyReviewed(c.ID); err == nil {
		t.Fatal("a patch applies once")
	}
}

func TestApplyPatchReportsConflictsAndRefusesAnotherRepository(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	other := draftRepo(t)
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: other, AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	if err := r.applyReviewed(c.ID); err == nil || !strings.Contains(err.Error(), "the patch is for") {
		t.Fatalf("another repository: %v", err)
	}
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: r.repo, AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	writeT(t, filepath.Join(r.repo, "auth.js"), "const a = 1\nsessionStorage.setItem('jwt', token)\nconst c = 3\n") // the agent moved on
	if err := r.applyReviewed(c.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := r.q.Get(c.ID)
	if got.Status != queue.StatusApplied || got.Note != "applied with conflicts in 1 file: auth.js" || !slices.Contains(got.PatchFlags, queue.FlagConflicts) {
		t.Fatalf("card %+v", got)
	}
	if body, _ := os.ReadFile(filepath.Join(r.repo, "auth.js")); !strings.Contains(string(body), "<<<<<<<") {
		t.Fatalf("auth.js = %q, want conflict markers", body)
	}
	eventually(t, "a conflict toast", func() bool { return len(r.srv.Calls("notification.show")) > 0 })
}

func TestDiscardAndDismissRemoveThePatchAndRecordALedgerLine(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	for _, discard := range []func(string) error{r.e.DiscardPatch, r.e.Dismiss} {
		if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
			t.Fatal(err)
		}
		c := r.patchCard(t)
		r.draftsDone(t)
		if err := discard(c.ID); err != nil {
			t.Fatal(err)
		}
		if got, _ := r.q.Get(c.ID); got.Status != queue.StatusDismissed {
			t.Fatalf("card %+v", got)
		}
		if _, err := r.patches.Get(c.PatchID); !errors.Is(err, patchstore.ErrNotFound) {
			t.Fatalf("meta left: %v", err)
		}
		if _, err := os.Stat(r.patches.PatchPath(c.PatchID)); !os.IsNotExist(err) {
			t.Fatal("patch file left")
		}
		if r.refs(t) != "" {
			t.Fatal("snapshot ref left")
		}
	}
	eventually(t, "two discarded ledger lines", func() bool {
		l, _ := r.mem.Ledger(testProject)
		return len(l) == 2 && l[0].Kind == memory.OutcomeDiscarded && l[1].Kind == memory.OutcomeDiscarded
	})
	if es, _ := r.mem.List(testProject, memory.Filter{Scope: memory.ScopeDot, Dot: "security"}); len(es) != 0 {
		t.Fatalf("a discarded patch says nothing about the advice: %+v", es)
	}
}

func TestSendingAPatchDeliversWhereItIsNotTheDiff(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	if err := r.e.SendCards([]string{c.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "prompt sent", func() bool { return len(r.srv.Calls("agent.prompt")) == 1 })
	text := r.srv.Calls("agent.prompt")[0].Params["text"].(string)
	want := "[● Security dot] Patch ready: 1 file +1 −1. Review with git apply --stat \"" + r.patches.PatchPath(c.PatchID) + "\""
	if !strings.HasPrefix(text, want) || strings.Contains(text, "diff --git") || strings.Contains(text, "setCookie") {
		t.Fatalf("prompt %q", text)
	}
	eventually(t, "meta sent", func() bool { m, _ := r.patches.Get(c.PatchID); return m.Status == patchstore.StatusSent })
	if r.refs(t) == "" {
		t.Fatal("a sent patch keeps its ref so the agent can still apply it three ways")
	}
	if err := r.e.Edit(c.ID, "x"); err == nil {
		t.Fatal("patch cards can't be edited")
	}
}

func TestEmptyRejectedAndFailedDraftsLeaveNothingBehind(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.attach(t, "security")
	cases := []struct {
		name    string
		edit    func(string)
		err     error
		outcome string
		detail  string
		status  string // "" = no metadata kept
	}{
		{"empty", func(string) {}, nil, state.OutcomePass, "draft produced no changes", ""},
		{"escape", func(dir string) {
			if err := os.Symlink("/etc/passwd", filepath.Join(dir, "evil")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}, nil, state.OutcomeError, "patch rejected", patchstore.StatusRejected},
		{"failed", nil, errors.New("claude timed out"), state.OutcomeError, "draft failed: claude timed out", ""},
	}
	for _, c := range cases {
		r.dr.set(c.edit, c.err)
		id, err := r.e.Draft(context.Background(), pane, "security", "fix", "")
		if err != nil {
			t.Fatal(err)
		}
		r.draftsDone(t)
		eventually(t, c.name+": feed entry", func() bool { return r.feedHasDraft(c.outcome, c.detail) })
		for _, x := range r.q.Pending(pane) {
			if x.Kind == queue.KindPatch {
				t.Fatalf("%s: a card was made: %+v", c.name, x)
			}
		}
		m, merr := r.patches.Get(id)
		if (c.status == "" && !errors.Is(merr, patchstore.ErrNotFound)) || (c.status != "" && m.Status != c.status) {
			t.Fatalf("%s: meta %+v %v", c.name, m, merr)
		}
		if _, err := os.Stat(r.patches.PatchPath(id)); !os.IsNotExist(err) {
			t.Fatalf("%s: a patch file was kept", c.name)
		}
		if r.refs(t) != "" {
			t.Fatalf("%s: snapshot ref left", c.name)
		}
		if ents, _ := os.ReadDir(r.wts); len(ents) != 0 {
			t.Fatalf("%s: worktree left: %v", c.name, ents)
		}
	}
}

func TestEngineStartCleansUpAnInterruptedDraft(t *testing.T) {
	var id, dir string
	r := newDraftRig(t, draftOpts{before: func(p *patchstore.Store, wts string, g worktree.Git, repo string) {
		id = worktree.NewID()
		s, err := worktree.TakeSnapshot(context.Background(), g, repo, id, 0)
		if err != nil {
			t.Fatal(err)
		}
		dir = filepath.Join(wts, id)
		if _, err := worktree.Add(context.Background(), g, s, dir); err != nil {
			t.Fatal(err)
		}
		if err := p.Put(patchstore.Meta{ID: id, Dot: "security", Pane: pane, Repo: repo, Status: patchstore.StatusDrafting}); err != nil {
			t.Fatal(err)
		}
	}}, writeDot("Security"))
	eventually(t, "the interrupted draft cleaned up", func() bool {
		_, merr := r.patches.Get(id)
		_, derr := os.Stat(dir)
		return errors.Is(merr, patchstore.ErrNotFound) && os.IsNotExist(derr)
	})
	// The record and directory go first; the ref drop and `git worktree
	// prune` follow, so git is polled without failing on a list that races
	// the prune.
	eventuallyWhy(t, "ref and worktree metadata gone", func() (bool, string) {
		list, err := gitTry(t, r.repo, "worktree", "list", "--porcelain")
		if err != nil {
			return false, err.Error()
		}
		refs, err := gitTry(t, r.repo, "for-each-ref", "--format=%(refname)", worktree.RefPrefix)
		if err != nil {
			return false, err.Error()
		}
		if refs != "" || strings.Count(list, "worktree ") != 1 {
			return false, "refs: " + refs + "\n" + list
		}
		return true, ""
	})
}

// Fix round 1: a wait that times out says what it last saw (the draft
// cleanup wait: the last git error).
func TestWaitForReportsTheLastReason(t *testing.T) {
	ftb := &fatalTB{TB: t}
	waitFor(ftb, 30*time.Millisecond, "metadata gone", func() (bool, string) { return false, "git [worktree list]: exit status 128" })
	if !strings.Contains(ftb.msg, "metadata gone") || !strings.Contains(ftb.msg, "exit status 128") {
		t.Fatalf("timeout message %q", ftb.msg)
	}
}

// fatalTB records Fatalf instead of failing the test.
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper()                        {}
func (f *fatalTB) Fatalf(format string, a ...any) { f.msg = fmt.Sprintf(format, a...) }

// A write-capable dot asked through MCP (ask_dot, ask_council), summoned, or
// seated in a council runs read-only like any dot: no write tools, no
// worktree, nothing changes on disk (decision 5; spec §10: no council patch).
func TestAsksCouncilsAndSummonsOfAWriteDotNeverWrite(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"), writeDot("Backend"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security", "backend")
	r.reviewsDone(t, 2) // attaching reviews the newest turn; an ask waits for it
	if _, err := r.e.Ask(context.Background(), pane, "security", "is the token safe?", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.AskCouncil(context.Background(), pane, "is the token safe?"); err != nil {
		t.Fatal(err)
	}
	if err := r.e.Summon(context.Background(), pane, "backend", "look again"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.Council(context.Background(), pane, "review the auth change", nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the council and the summon finished", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		r.dr.mu.Lock()
		n := len(r.dr.reqs)
		r.dr.mu.Unlock()
		return p.Council == nil && n >= 6 // ask 1 + ask_council 2 + summon 1 + council 2 (+ chairs)
	})
	r.dr.mu.Lock()
	defer r.dr.mu.Unlock()
	for i, req := range r.dr.reqs {
		if req.Write || req.Pass == backend.PassWrite || backend.WriteMode(req) {
			t.Errorf("run %d asked for write tools: %+v", i, req)
		}
		if strings.HasPrefix(req.Cwd, r.wts) {
			t.Errorf("run %d ran in a draft worktree: %s", i, req.Cwd)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(r.repo, "auth.js")); strings.Contains(string(got), "setCookie") {
		t.Fatal("a read-only run changed the user's files")
	}
	if ents, _ := os.ReadDir(r.wts); len(ents) != 0 || r.refs(t) != "" {
		t.Fatalf("a read-only run made a worktree or a snapshot: %v", ents)
	}
}

// A draft is one call: it is refused once the dot's call budget is spent.
func TestDraftIsRefusedWhenTheBudgetIsSpent(t *testing.T) {
	d := writeDot("Security")
	d.Budget.MaxCallsPerSession = 2
	r := newDraftRig(t, draftOpts{}, d)
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	r.reviewsDone(t, 1) // the attach review is call 1
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	r.patchCard(t)
	r.draftsDone(t)
	if n := r.usage.Calls(pane, "security"); n != 2 {
		t.Fatalf("calls %d, want 2: the draft is one call", n)
	}
	if _, err := r.e.Draft(context.Background(), pane, "security", "again", ""); !errors.Is(err, ErrBudget) {
		t.Fatalf("over budget: %v, want ErrBudget", err)
	}
}
