package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/config"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/worktree"
)

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
	// Detaching the last dot resets the pane's usage (as a closed pane does):
	// the draft ending afterwards must not charge it again.
	if got := r.usage.Calls(pane, "security"); calls == 0 || got != 0 {
		t.Fatalf("a closed pane was charged: %d calls after the reset (was %d)", got, calls)
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
// swept at start together with its snapshot ref; a patch with a card stays,
// whether the record names the card or only the card names the patch (a crash
// between Queue.Add and the record's card write).
func TestStartSweepsOrphanPatchesButKeepsPatchesWithCards(t *testing.T) {
	var keptNamed, keptByCard, orphan string
	r := newDraftRig(t, draftOpts{
		before: func(p *patchstore.Store, wts string, g worktree.Git, repo string) {
			ctx := context.Background()
			keptNamed, keptByCard, orphan = worktree.NewID(), worktree.NewID(), worktree.NewID()
			for _, id := range []string{keptNamed, keptByCard, orphan} {
				s, err := worktree.TakeSnapshot(ctx, g, repo, id, 0)
				if err != nil {
					t.Fatal(err)
				}
				m := patchstore.Meta{ID: id, Dot: "security", Pane: pane, Repo: repo, Ref: s.Ref, Snapshot: s.Commit, Status: patchstore.StatusReady}
				if err := p.PutPatch(m, []byte("diff --git a/x b/x\n")); err != nil {
					t.Fatal(err)
				}
			}
		},
		deps: func(d *Deps) {
			for _, id := range []string{keptNamed, keptByCard} {
				c, err := d.Queue.Add(queue.Card{Kind: queue.KindPatch, PatchID: id, Dot: "security", DotName: "Security", PaneID: pane, Body: "x", Mode: "queue"})
				if err != nil {
					t.Fatal(err)
				}
				if id == keptNamed {
					if _, err := d.Patches.Update(id, func(m *patchstore.Meta) { m.Card = c.ID }); err != nil {
						t.Fatal(err)
					}
				}
			}
		},
	}, writeDot("Security"))
	eventually(t, "the orphan patch swept", func() bool {
		_, err := r.patches.Get(orphan)
		return errors.Is(err, patchstore.ErrNotFound)
	})
	if _, err := os.Stat(r.patches.PatchPath(orphan)); !os.IsNotExist(err) {
		t.Fatal("the orphan's patch file is left")
	}
	eventually(t, "the orphan's ref dropped", func() bool { return !strings.Contains(r.refs(t), orphan) })
	for _, id := range []string{keptNamed, keptByCard} {
		if _, err := r.patches.Get(id); err != nil {
			t.Fatalf("a patch with a live card was swept: %v", err)
		}
		if !strings.Contains(r.refs(t), id) {
			t.Fatalf("the ref of %s was dropped", id)
		}
	}
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
	if err := os.WriteFile(path, []byte("write_enabled = [oops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.e.ReloadConfig()
	if r.e.config().WriteEnabled {
		t.Fatal("a malformed config must turn write_enabled off")
	}
}
