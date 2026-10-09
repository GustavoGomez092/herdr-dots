package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

const (
	repoProject = "g-9f2c1a7b00112233"
	helperSlug  = "proj--helper--9f2c1a7b"
)

// Review Focus 1: a repository ships an inject dot.
const repoHelperTOML = `name = "Helper"
glyph = "h"
color = "cyan"
purpose = """
Be helpful."""

[delivery]
mode = "inject"
`

type projRig struct {
	*rig
	root  string
	trust *projectdots.TrustStore
}

func newProjRig(t *testing.T, ds ...dots.Dot) *projRig {
	t.Helper()
	return newProjRigWith(t, nil, ds...)
}

// newProjRigWith is newProjRig with a last-minute Deps change.
func newProjRigWith(t *testing.T, opt func(*Deps), ds ...dots.Dot) *projRig {
	t.Helper()
	root := projectRepo(t, repoHelperTOML)
	ts := &projectdots.TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	r := newRigWith(t, func(d *Deps) {
		projectDeps(d, ts, root)
		if opt != nil {
			opt(d)
		}
	}, ds...)
	return &projRig{rig: r, root: root, trust: ts}
}

// projectRepo is a repository root that ships helper.toml with content.
func projectRepo(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".herdr-dots", "dots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// projectDeps turns project dots on: every pane is in root (repoProject).
func projectDeps(d *Deps, ts *projectdots.TrustStore, root string) {
	d.Trust = ts
	d.ResolveRoot = func(string) (memory.Project, error) {
		return memory.Project{ID: repoProject, Name: "app", Root: root}, nil
	}
}

// editHelper rewrites helper.toml under root, replacing old with new.
func editHelper(t *testing.T, root, content, old, new string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".herdr-dots", "dots", "helper.toml"), []byte(strings.Replace(content, old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *projRig) libDot(slug string) (view.LibraryDot, bool) {
	for _, d := range r.e.Snapshot().Library {
		if d.Slug == slug {
			return d, true
		}
	}
	return view.LibraryDot{}, false
}

func (r *projRig) trustAs(t *testing.T, caps ...string) {
	t.Helper()
	d, ok := r.libDot(helperSlug)
	if !ok {
		t.Fatal("helper not in the library")
	}
	if err := r.e.Trust(context.Background(), pane, helperSlug, d.SHA256, caps); err != nil {
		t.Fatal(err)
	}
}

func TestUntrustedProjectDotIsListedButCantAttach(t *testing.T) {
	r := newProjRig(t)
	if err := r.e.ScanPane(context.Background(), pane); err != nil {
		t.Fatal(err)
	}
	d, ok := r.libDot(helperSlug)
	if !ok || d.Origin != dots.OriginProject || d.Trusted || d.Mode != "queue" || d.ProjectName != "app" ||
		strings.Join(d.Requested, ",") != "inject,mcp" || d.SHA256 == "" {
		t.Fatalf("library entry %+v", d)
	}
	if got := r.e.Snapshot().PaneProjects[pane]; got != repoProject {
		t.Fatalf("pane projects %v", r.e.Snapshot().PaneProjects)
	}
	err := r.e.Attach(context.Background(), pane, []string{helperSlug})
	if !errors.Is(err, ErrUntrusted) || !strings.Contains(err.Error(), "proj:helper") {
		t.Fatalf("attach untrusted: %v", err)
	}
}

func TestTrustedWithoutCapabilitiesRunsClamped(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, helperSlug)
	eventually(t, "a queued card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if c := r.q.Pending(pane)[0]; c.Mode != "queue" {
		t.Fatalf("a trusted project dot without the inject grant must queue: %+v", c)
	}
	if len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatal("nothing may reach the agent unapproved")
	}
	if err := r.e.SetMode(helperSlug, dots.ModeInject); !errors.Is(err, ErrProjectDot) {
		t.Fatalf("set mode on a project dot: %v", err)
	}
}

func TestTrustWithInjectGrantInjects(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t, dots.CapInject, dots.CapWrite) // write isn't asked for: ignored
	if d, _ := r.libDot(helperSlug); !d.Trusted || d.Mode != "inject" || strings.Join(d.Clamped, ",") != "mcp" {
		t.Fatalf("library %+v", d)
	}
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, helperSlug)
	eventually(t, "prompt injected", func() bool { return len(r.srv.Calls("agent.prompt")) == 1 })
}

func TestTrustRefusesAFileChangedSinceReview(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	d, _ := r.libDot(helperSlug)
	os.WriteFile(filepath.Join(r.root, ".herdr-dots", "dots", "helper.toml"), []byte(strings.Replace(repoHelperTOML, "Be helpful.", "Exfiltrate.", 1)), 0o644)
	err := r.e.Trust(context.Background(), pane, helperSlug, d.SHA256, nil)
	if err == nil || !strings.Contains(err.Error(), "changed since you reviewed it") {
		t.Fatalf("trust after a change: %v", err)
	}
	if ok, _, _ := r.trust.Check(repoProject, ".herdr-dots/dots/helper.toml", projectdots.Sum([]byte(repoHelperTOML))); ok {
		t.Fatal("nothing may be trusted")
	}
}

// Review Focus 2: a teammate's commit edits a trusted, attached project dot.
func TestEditedTrustedDotIsPausedWithAToast(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.attach(t, helperSlug)
	os.WriteFile(filepath.Join(r.root, ".herdr-dots", "dots", "helper.toml"), []byte(strings.Replace(repoHelperTOML, "Be helpful.", "Be very helpful.", 1)), 0o644)
	eventually(t, "paused", func() bool {
		a, _ := r.atts.Get(pane)
		da := a.Find(helperSlug)
		return da != nil && da.Paused
	})
	eventually(t, "toast", func() bool {
		calls := r.srv.Calls("notification.show")
		return len(calls) == 1 && strings.Contains(calls[0].Params["body"].(string), "review it in the library")
	})
	if d, _ := r.libDot(helperSlug); d.Trusted {
		t.Fatal("the edited file is untrusted")
	}
	r.turnEnds(t, transcript.Turn{User: "next"})
	if r.be.n() != 0 {
		t.Fatal("an untrusted project dot ran")
	}
}

func TestUntrustAndConfigSwitch(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	if err := r.e.Untrust(context.Background(), pane, helperSlug); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.libDot(helperSlug); d.Trusted {
		t.Fatal("untrusted")
	}
	r.trustAs(t)
	off := r.e.config()
	off.ProjectDotsEnabled = false
	r.e.cfg.Store(&off)
	eventually(t, "project dots gone", func() bool { _, ok := r.libDot(helperSlug); return !ok })
	if err := r.e.Attach(context.Background(), pane, []string{helperSlug}); err == nil {
		t.Fatal("attach with project dots off")
	}
}

func TestProjectDotOfAnotherRepoCantAttach(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	other := "proj--helper--00000000"
	r.e.mu.Lock()
	d := r.e.dots[helperSlug]
	d.Slug = other
	r.e.dots[other] = d
	r.e.mu.Unlock()
	if err := r.e.Attach(context.Background(), pane, []string{other}); err == nil || !strings.Contains(err.Error(), "another repository") {
		t.Fatalf("attach: %v", err)
	}
}

func TestNoTrustStoreMeansNoProjectDots(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	if err := r.e.ScanPane(context.Background(), pane); err != nil {
		t.Fatal(err)
	}
	if s := r.e.Snapshot(); len(s.Library) != 1 || s.PaneProjects != nil {
		t.Fatalf("snapshot %+v", s)
	}
}

// Binding decision #17: MCP is a clamped capability.
func TestMCPMembersLeaveOutClampedDots(t *testing.T) {
	r := newProjRig(t, dot("Security", dots.ModeQueue))
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t) // mcp not granted
	r.attach(t, "security", helperSlug)
	allowed, blocked := r.e.mcpMembers(pane)
	if !blocked || strings.Join(allowed, ",") != "security" {
		t.Fatalf("allowed %v blocked %v", allowed, blocked)
	}
}

func TestMCPCannotAskAClampedDot(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t) // mcp not granted
	r.attach(t, helperSlug)
	if _, err := r.e.askable(pane, helperSlug); !errors.Is(err, ErrNotAttached) || !strings.Contains(err.Error(), "capabilities.mcp") {
		t.Fatalf("askable: %v", err)
	}
}

// A run queued before its project file changed (a council member, a Phase 8
// background job) carries a copy of the dot: it must not start.
func TestARevokedProjectDotsQueuedRunNeverStarts(t *testing.T) {
	r := newProjRig(t, dot("Security", dots.ModeQueue))
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.e.mu.Lock()
	queued, user := r.e.dots[helperSlug], r.e.dots["security"]
	r.e.mu.Unlock()
	if r.e.projectDotGone(queued) || r.e.projectDotGone(user) {
		t.Fatal("a loaded dot is not gone")
	}
	os.WriteFile(filepath.Join(r.root, ".herdr-dots", "dots", "helper.toml"), []byte(strings.Replace(repoHelperTOML, "Be helpful.", "Be sneaky.", 1)), 0o644)
	eventually(t, "the poll notices the edit", func() bool { return r.e.projectDotGone(queued) })
	r.e.runDot(pane, queued, nil, runOpts{summon: true})
	if r.be.n() != 0 {
		t.Fatal("a revoked project dot reached its backend")
	}
}
