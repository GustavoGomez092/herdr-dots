package engine

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/worktree"
)

// Write tools exist only in a draft run (spec: reviews, councils, MCP asks,
// memory passes and summons stay read-only). This is the source half of the
// guard: no engine file but draft.go may name PassWrite, set Request.Write,
// or build a backend.Request with a Write or Pass key; Request.Pass may only
// ever be assigned PassMemory or PassReview. The run-time half is
// TestAsksCouncilsAndSummonsOfAWriteDotNeverWrite and
// TestOnlyTheDraftRunAsksForWriteTools.
func TestOnlyDraftsAskForWriteTools(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "draft.go" {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(af, func(n ast.Node) bool {
			pos := func() string { return fset.Position(n.Pos()).String() }
			switch x := n.(type) {
			case *ast.Ident:
				if x.Name == "PassWrite" {
					t.Errorf("%s: PassWrite outside draft.go", pos())
				}
			case *ast.CompositeLit:
				if sel, ok := x.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Request" {
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && (k.Name == "Write" || k.Name == "Pass") {
								t.Errorf("%s: backend.Request literal sets %s outside draft.go", pos(), k.Name)
							}
						}
					}
				}
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					sel, ok := l.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					switch sel.Sel.Name {
					case "Write":
						t.Errorf("%s: assignment to .Write outside draft.go", pos())
					case "Pass":
						okRHS := false
						if len(x.Lhs) == len(x.Rhs) {
							if r, ok := x.Rhs[i].(*ast.SelectorExpr); ok && (r.Sel.Name == "PassMemory" || r.Sel.Name == "PassReview") {
								okRHS = true
							}
						}
						if !okRHS {
							t.Errorf("%s: .Pass assigned something other than PassMemory/PassReview outside draft.go", pos())
						}
					}
				}
			}
			return true
		})
	}
	if checked < 10 {
		t.Fatalf("only %d engine files checked: the glob is wrong", checked)
	}
}

// Run-time half: next to a draft, the attach review of the same write-capable
// dot is read-only, and exactly one run in all asked for write tools.
func TestOnlyTheDraftRunAsksForWriteTools(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	r.patchCard(t)
	r.draftsDone(t)
	r.dr.mu.Lock()
	defer r.dr.mu.Unlock()
	writes := 0
	for i, req := range r.dr.reqs {
		if req.Write || req.Pass == backend.PassWrite {
			writes++
			if !backend.WriteMode(req) || req.Learn || req.Force {
				t.Errorf("run %d: a draft must set Write and PassWrite together, with Learn and Force off: %+v", i, req)
			}
			continue
		}
		if req.Write || req.Pass == backend.PassWrite {
			t.Errorf("run %d is not a draft but asks for write tools: %+v", i, req)
		}
	}
	if writes != 1 || len(r.dr.reqs) < 2 {
		t.Fatalf("%d write runs of %d, want exactly one next to the review", writes, len(r.dr.reqs))
	}
}

func TestDraftArgsAreValidated(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"), dot("Style", "queue"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security", "style")
	call := func(a ipc.DraftArgs) error {
		raw, _ := json.Marshal(a)
		return r.e.Handle(context.Background(), ipc.CmdDraft, raw)
	}
	long := strings.Repeat("x", maxInstruction+1)
	cases := map[string]ipc.DraftArgs{
		"no pane":          {Slug: "security", Instruction: "fix"},
		"no slug":          {PaneID: pane, Instruction: "fix"},
		"not attached":     {PaneID: pane, Slug: "backend", Instruction: "fix"},
		"unattached pane":  {PaneID: "w9:p9", Slug: "security", Instruction: "fix"},
		"not write":        {PaneID: pane, Slug: "style", Instruction: "fix"},
		"too long":         {PaneID: pane, Slug: "security", Instruction: long},
		"far too long":     {PaneID: pane, Slug: "security", Instruction: strings.Repeat("y", 100000)},
		"empty":            {PaneID: pane, Slug: "security"},
		"card of nowhere":  {PaneID: pane, Slug: "security", FromCardID: "999"},
		"whitespace only ": {PaneID: pane, Slug: "security", Instruction: " \n\t "},
	}
	for name, a := range cases {
		if err := call(a); err == nil {
			t.Errorf("%s: accepted %+v", name, a)
		}
	}
	if len(r.dr.writes()) != 0 {
		t.Fatal("a refused draft must not run")
	}
	cfg := r.e.config()
	cfg.WriteEnabled = false
	r.e.cfg.Store(&cfg)
	if err := call(ipc.DraftArgs{PaneID: pane, Slug: "security", Instruction: "fix"}); !errors.Is(err, ErrWriteDisabled) {
		t.Fatalf("write_enabled off: %v", err)
	}
}

func TestAPaneThatClosesMidDraftGetsNoCard(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	gate := make(chan struct{})
	r.dr.mu.Lock()
	r.dr.gate, r.dr.edit = gate, fixJWT
	r.dr.mu.Unlock()
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	calls := r.usage.Calls(pane, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the draft runs", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Drafts) == 1 && len(r.dr.writes()) == 1
	})
	if err := r.e.Detach(pane, "security"); err != nil {
		t.Fatal(err)
	}
	close(gate)
	eventually(t, "the draft ended", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return len(r.e.drafts) == 0
	})
	for _, c := range r.q.All() {
		if c.Kind == queue.KindPatch && c.Status != queue.StatusDismissed {
			t.Fatalf("a card was made for a closed pane: %+v", c)
		}
	}
	if ms, _ := r.patches.List(); len(ms) != 0 {
		t.Fatalf("metadata left: %+v", ms)
	}
	if ents, _ := os.ReadDir(r.wts); len(ents) != 0 || r.refs(t) != "" {
		t.Fatalf("worktree or ref left: %v %q", ents, r.refs(t))
	}
	if got := r.usage.Calls(pane, "security"); got != calls {
		t.Fatalf("a closed pane was charged: %d calls, was %d", got, calls)
	}
}

func TestADraftTouchingASecretIsRejected(t *testing.T) {
	r := newDraftRig(t, draftOpts{cfg: func(c *config.Config) { c.ToolDenyGlobs = []string{"private/**"} }}, writeDot("Security"))
	r.attach(t, "security")
	for _, name := range []string{".env", "ID_RSA", filepath.Join("private", "x.txt")} {
		r.dr.set(func(dir string) {
			writeT(t, filepath.Join(dir, name), "SECRET=1\n")
		}, nil)
		id, err := r.e.Draft(context.Background(), pane, "security", "fix", "")
		if err != nil {
			t.Fatal(err)
		}
		r.draftsDone(t)
		m, err := r.patches.Get(id)
		if err != nil || m.Status != patchstore.StatusRejected {
			t.Fatalf("%s: meta %+v %v", name, m, err)
		}
		for _, x := range r.q.Pending(pane) {
			if x.Kind == queue.KindPatch {
				t.Fatalf("%s: a card was made", name)
			}
		}
		if _, err := os.Stat(r.patches.PatchPath(id)); !os.IsNotExist(err) {
			t.Fatalf("%s: a rejected patch was stored", name)
		}
	}
}

func TestEngineStopMidDraftLeavesNothingToApply(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	gate := make(chan struct{}) // never opened: the run ends with the context
	r.dr.mu.Lock()
	r.dr.gate, r.dr.edit = gate, fixJWT
	r.dr.mu.Unlock()
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the draft runs", func() bool { return len(r.dr.writes()) == 1 })
	r.stop()
	r.e.Wait()
	for _, c := range r.q.All() {
		if c.Kind == queue.KindPatch {
			t.Fatalf("a card after shutdown: %+v", c)
		}
	}
	if ms, _ := r.patches.List(); len(ms) != 0 {
		t.Fatalf("metadata left: %+v", ms)
	}
	if ents, _ := os.ReadDir(r.wts); len(ents) != 0 || r.refs(t) != "" {
		t.Fatalf("worktree or ref left: %v %q", ents, r.refs(t))
	}
	for _, f := range r.e.d.Feed.Recent(pane, 50) {
		if strings.Contains(f.Detail, "draft failed") {
			t.Fatalf("a shutdown is not a failed draft: %+v", f)
		}
	}
}

// A ready patch whose card was never made (a crash between the two writes) is
// swept at start together with its snapshot ref; a patch with a card stays.
func TestStartSweepsOrphanPatchesButKeepsPatchesWithCards(t *testing.T) {
	var orphan, kept string
	r := newDraftRig(t, draftOpts{before: func(p *patchstore.Store, wts string, g worktree.Git, repo string) {
		ctx := context.Background()
		orphan, kept = worktree.NewID(), worktree.NewID()
		for _, id := range []string{orphan, kept} {
			s, err := worktree.TakeSnapshot(ctx, g, repo, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			m := patchstore.Meta{ID: id, Dot: "security", Pane: pane, Repo: repo, Ref: s.Ref, Snapshot: s.Commit, Status: patchstore.StatusReady}
			if err := p.PutPatch(m, []byte("diff --git a/x b/x\n")); err != nil {
				t.Fatal(err)
			}
		}
	}}, writeDot("Security"))
	_ = kept
	eventually(t, "the orphan patch swept", func() bool {
		_, err := r.patches.Get(orphan)
		return errors.Is(err, patchstore.ErrNotFound)
	})
	if _, err := os.Stat(r.patches.PatchPath(orphan)); !os.IsNotExist(err) {
		t.Fatal("the orphan's patch file is left")
	}
	eventually(t, "the orphan's ref dropped", func() bool {
		return !strings.Contains(r.refs(t), orphan)
	})
}

// A malformed config.toml turns write_enabled off (fail closed), like inject.
func TestAMalformedConfigTurnsWriteOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("write_enabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newDraftRig(t, draftOpts{cfg: func(c *config.Config) { c.WriteEnabled = true }}, writeDot("Security"))
	r.e.d.ConfigPath = path
	r.e.ReloadConfig()
	if !r.e.config().WriteEnabled {
		t.Fatal("a good config keeps write on")
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte("write_enabled = [oops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.e.ReloadConfig()
	if r.e.config().WriteEnabled {
		t.Fatal("a malformed config must turn write_enabled off")
	}
}

var _ = memory.OutcomeApplied
