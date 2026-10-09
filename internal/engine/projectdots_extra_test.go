package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
)

const helperRel = ".herdr-dots/dots/helper.toml"

// noPoll: no poll ever runs, so only a run-time check can see a change.
func noPoll(d *Deps) { d.PollInterval = time.Hour }

func (r *projRig) loaded(t *testing.T, slug string) dots.Dot {
	t.Helper()
	r.e.mu.Lock()
	defer r.e.mu.Unlock()
	d, ok := r.e.dots[slug]
	if !ok {
		t.Fatalf("%s is not loaded", slug)
	}
	return d
}

// Fresh trust on every run: a run queued before its file changed is skipped
// even when no poll has noticed the edit yet.
func TestAnEditNoPollSawYetStillStopsTheRun(t *testing.T) {
	r := newProjRigWith(t, noPoll)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	queued := r.loaded(t, helperSlug)
	editHelper(t, r.root, repoHelperTOML, "Be helpful.", "Be sneaky.")
	r.e.runDot(pane, queued, nil, runOpts{summon: true})
	if r.be.n() != 0 {
		t.Fatal("a changed project dot reached its backend")
	}
	if d, _ := r.libDot(helperSlug); d.Trusted {
		t.Fatal("the run-time check must also refresh the library")
	}
}

// Trust revoked outside the engine (herdr-dots dots untrust): the file is
// the same, the trust store is not.
func TestTrustRevokedElsewhereStopsTheNextRun(t *testing.T) {
	r := newProjRigWith(t, noPoll)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	queued := r.loaded(t, helperSlug)
	r.e.runDot(pane, queued, nil, runOpts{summon: true})
	if r.be.n() != 1 {
		t.Fatalf("a trusted project dot runs: %d calls", r.be.n())
	}
	if _, err := r.trust.Revoke(repoProject, helperRel); err != nil {
		t.Fatal(err)
	}
	r.e.runDot(pane, queued, nil, runOpts{summon: true})
	if r.be.n() != 1 {
		t.Fatal("a project dot whose trust was revoked ran")
	}
}

// Background runs (research, scheduled, commit) re-check trust too, and a
// skipped scheduled slot stays unclaimed.
func TestAChangedProjectDotNeverRunsInTheBackground(t *testing.T) {
	root := projectRepo(t, repoHelperTOML)
	ts := &projectdots.TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	r := newProRig(t, proOpts{deps: func(d *Deps) {
		projectDeps(d, ts, root)
		noPoll(d)
	}})
	if err := r.e.ScanPane(context.Background(), pane); err != nil {
		t.Fatal(err)
	}
	item, ok := projectdots.ScanProject(root, repoProject, ts).Find(helperSlug)
	if !ok {
		t.Fatal("helper not scanned")
	}
	if err := r.e.Trust(context.Background(), pane, helperSlug, projectdots.Hex(item.Sum), nil); err != nil {
		t.Fatal(err)
	}
	r.e.mu.Lock()
	d := r.e.dots[helperSlug]
	r.e.mu.Unlock()
	var claims atomic.Int32
	job := func() bgJob {
		j := scheduledJob(r, d)
		j.claim = func() bool { claims.Add(1); return true }
		return j
	}
	r.e.runBackground(job())
	if r.be.n() != 1 || claims.Load() != 1 {
		t.Fatalf("a trusted project dot runs in the background: %d calls, %d claims", r.be.n(), claims.Load())
	}
	editHelper(t, root, repoHelperTOML, "Be helpful.", "Be sneaky.")
	r.e.runBackground(job())
	if r.be.n() != 1 || claims.Load() != 1 {
		t.Fatalf("a changed project dot ran in the background (%d calls) or claimed its slot (%d)", r.be.n(), claims.Load())
	}
}

// A draft (write tools) of a project dot whose file changed after it was
// loaded never reaches the backend.
func TestAChangedProjectDotNeverDrafts(t *testing.T) {
	const writer = `name = "Helper"
glyph = "h"
purpose = "Fix things."

[capabilities]
write = true
`
	root := projectRepo(t, writer)
	ts := &projectdots.TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	r := newDraftRig(t, draftOpts{deps: func(d *Deps) {
		projectDeps(d, ts, root)
		noPoll(d)
	}})
	if err := r.e.ScanPane(context.Background(), pane); err != nil {
		t.Fatal(err)
	}
	item, _ := projectdots.ScanProject(root, repoProject, ts).Find(helperSlug)
	if err := r.e.Trust(context.Background(), pane, helperSlug, projectdots.Hex(item.Sum), []string{dots.CapWrite}); err != nil {
		t.Fatal(err)
	}
	r.attach(t, helperSlug)
	r.reviewsDone(t, 1)
	editHelper(t, root, writer, "Fix things.", "Break things.")
	if _, err := r.e.Draft(context.Background(), pane, helperSlug, "fix", ""); err != nil {
		t.Fatal(err) // e.dots still holds the old copy: no poll ran
	}
	r.draftsDone(t)
	r.dr.mu.Lock()
	defer r.dr.mu.Unlock()
	for _, req := range r.dr.reqs {
		if backend.WriteMode(req) {
			t.Fatal("a changed project dot started a draft run")
		}
	}
}

// Trust given in another checkout of the same repository (clones share the
// project id) is shown with where it was given; paths are repo-relative or
// home-abbreviated.
func TestTrustFromAnotherCheckoutIsShown(t *testing.T) {
	r := newProjRig(t)
	elsewhere := filepath.Join(t.TempDir(), "other-clone", ".herdr-dots", "dots", "helper.toml")
	if err := r.trust.Trust(repoProject, helperRel, elsewhere, projectdots.Sum([]byte(repoHelperTOML)), nil); err != nil {
		t.Fatal(err)
	}
	_ = r.e.ScanPane(context.Background(), pane)
	d, ok := r.libDot(helperSlug)
	if !ok || !d.Trusted || d.Rel != helperRel || d.TrustedAt != shortHome(elsewhere) || d.TrustedAt == "" {
		t.Fatalf("library %+v", d)
	}
	r.trustAs(t) // trusted again, here
	if d, _ := r.libDot(helperSlug); !d.Trusted || d.TrustedAt != "" {
		t.Fatalf("trusted in this checkout: %+v", d)
	}
}

func TestShortHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "me")
	cases := map[string]string{
		filepath.Join(home, "src", "app", "x.toml"): "~" + string(filepath.Separator) + filepath.Join("src", "app", "x.toml"),
		home:                       "~",
		filepath.Join("/opt", "x"): filepath.Join("/opt", "x"),
		home + "other":             home + "other",
	}
	for in, want := range cases {
		if got := shortHomeIn(in, home); got != want {
			t.Errorf("shortHomeIn(%q) = %q, want %q", in, got, want)
		}
	}
	if got := shortHomeIn("/a/b", ""); got != "/a/b" {
		t.Errorf("no home: %q", got)
	}
}

// Carry-over 6: an MCP ask of an untrusted project dot is refused clearly,
// and a council with every attached dot clamped is refused.
func TestMCPRefusesUntrustedAndClampedProjectDots(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.attach(t, helperSlug)
	if _, err := r.e.AskCouncil(context.Background(), pane, "is this safe?"); err == nil || !strings.Contains(err.Error(), "available to the MCP server") {
		t.Fatalf("council of clamped dots: %v", err)
	}
	if err := r.e.Untrust(context.Background(), pane, helperSlug); err != nil {
		t.Fatal(err)
	}
	_, err := r.e.askable(pane, helperSlug)
	if !errors.Is(err, ErrUntrusted) || !errors.Is(err, ErrNotAttached) || !strings.Contains(err.Error(), "proj:helper") {
		t.Fatalf("askable untrusted: %v", err)
	}
	if r.be.n() != 0 {
		t.Fatal("nothing ran")
	}
}

// Trust and untrust over IPC bind the hash the developer saw; reload_dots
// with a pane scans its repository.
func TestTrustOverIPC(t *testing.T) {
	r := newProjRig(t)
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	if err := r.e.Handle(context.Background(), ipc.CmdReloadDots, raw(ipc.ReloadArgs{PaneID: pane})); err != nil {
		t.Fatal(err)
	}
	d, ok := r.libDot(helperSlug)
	if !ok || d.Trusted {
		t.Fatalf("reload_dots with a pane scans it: %+v", d)
	}
	if err := r.e.Handle(context.Background(), ipc.CmdReloadDots, nil); err != nil {
		t.Fatalf("reload_dots without arguments: %v", err)
	}
	bad := ipc.TrustArgs{PaneID: pane, Slug: helperSlug, SHA256: strings.Repeat("0", 64)}
	if err := r.e.Handle(context.Background(), ipc.CmdTrust, raw(bad)); err == nil || !strings.Contains(err.Error(), "changed since you reviewed it") {
		t.Fatalf("trust with another hash: %v", err)
	}
	good := ipc.TrustArgs{PaneID: pane, Slug: helperSlug, SHA256: d.SHA256, Capabilities: []string{dots.CapMCP}}
	if err := r.e.Handle(context.Background(), ipc.CmdTrust, raw(good)); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.libDot(helperSlug); !d.Trusted || len(d.Clamped) != 1 || d.Clamped[0] != dots.CapInject {
		t.Fatalf("trusted with mcp: %+v", d)
	}
	if err := r.e.Handle(context.Background(), ipc.CmdUntrust, raw(ipc.DotArgs{PaneID: pane, Slug: helperSlug})); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.libDot(helperSlug); d.Trusted {
		t.Fatal("untrusted over IPC")
	}
}

// Pane generations: a pane dropped while its repository was being resolved
// (git runs off every lock) leaves no stale pane → repository entry.
func TestAPaneDroppedDuringResolveIsNotRecorded(t *testing.T) {
	var drop atomic.Bool
	var r *projRig
	r = newProjRigWith(t, func(d *Deps) {
		inner := d.ResolveRoot
		d.ResolveRoot = func(cwd string) (memory.Project, error) {
			if drop.Load() {
				r.e.dropPane(pane)
			}
			return inner(cwd)
		}
	})
	drop.Store(true)
	_ = r.e.ScanPane(context.Background(), pane)
	if got := r.e.Snapshot().PaneProjects[pane]; got != "" {
		t.Fatalf("a dropped pane is mapped to %q", got)
	}
	drop.Store(false)
	_ = r.e.ScanPane(context.Background(), pane)
	if got := r.e.Snapshot().PaneProjects[pane]; got != repoProject {
		t.Fatalf("pane projects %v", r.e.Snapshot().PaneProjects)
	}
}

// The same file reached another way (a symlinked checkout path, as the CLI
// records filepath.Abs) is this checkout, not another one.
func TestTrustThroughASymlinkIsThisCheckout(t *testing.T) {
	r := newProjRig(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(r.root, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	via := filepath.Join(link, filepath.FromSlash(helperRel))
	if err := r.trust.Trust(repoProject, helperRel, via, projectdots.Sum([]byte(repoHelperTOML)), nil); err != nil {
		t.Fatal(err)
	}
	_ = r.e.ScanPane(context.Background(), pane)
	if d, _ := r.libDot(helperSlug); !d.Trusted || d.TrustedAt != "" {
		t.Fatalf("library %+v", d)
	}
}
