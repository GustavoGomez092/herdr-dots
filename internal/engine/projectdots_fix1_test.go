package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// Fix 1: a repository file name can't put terminal escapes on screen (the
// reviewer's probe: an OSC 52 clipboard write in a .toml name).
func TestAHostileProjectFileNameIsSanitized(t *testing.T) {
	r := newProjRig(t)
	bad := filepath.Join(r.root, ".herdr-dots", "dots", "x\x1b]52;c;aGk=\a.toml")
	if err := os.WriteFile(bad, []byte(repoHelperTOML), 0o644); err != nil {
		t.Skipf("this file system refuses the name: %v", err)
	}
	_ = r.e.ScanPane(context.Background(), pane)
	inv := r.e.Snapshot().Invalid
	if len(inv) == 0 {
		t.Fatal("the badly named file is reported")
	}
	for _, x := range inv {
		for _, s := range []string{x.File, x.Error} {
			if strings.ContainsAny(s, "\x1b\a") {
				t.Fatalf("raw control characters reach the snapshot: %q", s)
			}
		}
	}
}

// Fix 2: an inject card waiting for the agent is not injected once its dot
// is untrusted: it goes back to the queue for the developer.
func TestAWaitingInjectCardIsNotInjectedAfterUntrust(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t, dots.CapInject)
	gate := make(chan struct{})
	r.be.mu.Lock()
	r.be.gate = gate
	r.be.mu.Unlock()
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, helperSlug)
	eventually(t, "the review is running", func() bool { return r.be.n() == 1 })
	busy := herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusWorking, Cwd: "/repo",
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}}
	r.srv.SetPane(busy) // the agent is working: an inject card has to wait
	close(gate)
	eventually(t, "an auto card waits", func() bool {
		p := r.q.Pending(pane)
		return len(p) == 1 && p[0].Status == queue.StatusAuto
	})
	if err := r.e.Untrust(context.Background(), pane, helperSlug); err != nil {
		t.Fatal(err)
	}
	if p := r.q.Pending(pane); len(p) != 1 || p[0].Status != queue.StatusPending {
		t.Fatalf("the waiting card goes back to the queue: %+v", p)
	}
	busy.AgentStatus = herdr.StatusIdle
	r.srv.SetPane(busy)
	r.srv.Push(pane, herdr.StatusIdle)
	time.Sleep(150 * time.Millisecond) // a poll or two
	if n := len(r.srv.Calls("agent.prompt")); n != 0 {
		t.Fatalf("an untrusted dot's card was injected (%d prompts)", n)
	}
}

// twoCheckouts is one repository (one project id) checked out twice, A and
// B, each with its own helper.toml; pane is in A and pane2 in B.
type twoCheckouts struct {
	*rig
	a, b  string
	trust *projectdots.TrustStore
}

const pane2 = "w1:p2"

func newTwoCheckouts(t *testing.T, aContent, bContent string) *twoCheckouts {
	t.Helper()
	a, b := projectRepo(t, aContent), projectRepo(t, bContent)
	ts := &projectdots.TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	r := newRigWith(t, func(d *Deps) {
		d.Trust = ts
		d.ResolveRoot = func(cwd string) (memory.Project, error) {
			root := a
			if cwd == "/b" {
				root = b
			}
			return memory.Project{ID: repoProject, Name: "app", Root: root}, nil
		}
	})
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/a",
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane2, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/b",
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s2"}})
	r.setReader("s2", &fakeReader{})
	for _, p := range []string{pane, pane2} {
		if err := r.e.ScanPane(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	return &twoCheckouts{rig: r, a: a, b: b, trust: ts}
}

func (r *twoCheckouts) entry(t *testing.T, root string) view.LibraryDot {
	t.Helper()
	for _, d := range r.e.Snapshot().Library {
		if d.Slug == helperSlug && d.Root == root {
			return d
		}
	}
	t.Fatalf("no library entry for %s at %s", helperSlug, root)
	return view.LibraryDot{}
}

func (r *twoCheckouts) trustAt(t *testing.T, paneID, root string) {
	t.Helper()
	raw, _ := json.Marshal(ipc.TrustArgs{PaneID: paneID, Root: root, Slug: helperSlug, SHA256: r.entry(t, root).SHA256})
	if err := r.e.Handle(context.Background(), ipc.CmdTrust, raw); err != nil {
		t.Fatal(err)
	}
}

// Ruling P9-R13: two checkouts of one repository each see, trust and run
// their own project dot files.
func TestEachCheckoutRunsItsOwnProjectDot(t *testing.T) {
	bTOML := strings.Replace(repoHelperTOML, "Be helpful.", "Be careful.", 1)
	r := newTwoCheckouts(t, repoHelperTOML, bTOML)
	s := r.e.Snapshot()
	if s.PaneRoots[pane] != r.a || s.PaneRoots[pane2] != r.b || s.PaneProjects[pane] != repoProject || s.PaneProjects[pane2] != repoProject {
		t.Fatalf("pane roots %v projects %v", s.PaneRoots, s.PaneProjects)
	}
	ea, eb := r.entry(t, r.a), r.entry(t, r.b)
	if ea.SHA256 == eb.SHA256 || ea.Trusted || eb.Trusted {
		t.Fatalf("two entries with their own content: %+v %+v", ea, eb)
	}
	// Trust addressed at a checkout the pane is not in is refused.
	raw, _ := json.Marshal(ipc.TrustArgs{PaneID: pane, Root: r.b, Slug: helperSlug, SHA256: eb.SHA256})
	if err := r.e.Handle(context.Background(), ipc.CmdTrust, raw); err == nil {
		t.Fatal("trust for B through a pane in A")
	}
	r.trustAt(t, pane2, r.b)
	if !r.entry(t, r.b).Trusted || r.entry(t, r.a).Trusted {
		t.Fatal("trusting B binds B's bytes only")
	}
	if ok, _, _ := r.trust.Check(repoProject, helperRel, projectdots.Sum([]byte(bTOML))); !ok {
		t.Fatal("B's sum is in the store")
	}
	if err := r.e.Attach(context.Background(), pane, []string{helperSlug}); err == nil {
		t.Fatal("A's copy is untrusted: it can't attach in A")
	}
	r.trustAt(t, pane, r.a)
	r.readers["s1"].add(transcript.Turn{User: "in A"})
	r.readers["s2"].add(transcript.Turn{User: "in B"})
	for _, p := range []string{pane, pane2} {
		if err := r.e.Attach(context.Background(), p, []string{helperSlug}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "both reviews ran", func() bool { return r.be.n() == 2 })
	r.be.mu.Lock()
	defer r.be.mu.Unlock()
	got := map[string]string{}
	for _, req := range r.be.calls {
		got[req.Turns[len(req.Turns)-1].User] = req.Purpose
	}
	if got["in A"] != "Be helpful." || got["in B"] != "Be careful." {
		t.Fatalf("each pane runs its own checkout's dot: %v", got)
	}
}

// Identical bytes trusted in one checkout are trusted in the other (trust is
// per project, file and content), and the other shows where.
func TestIdenticalContentTrustedInOneCheckoutIsTrustedInTheOther(t *testing.T) {
	r := newTwoCheckouts(t, repoHelperTOML, repoHelperTOML)
	r.trustAt(t, pane, r.a)
	_ = r.e.ScanPane(context.Background(), pane2)
	eb := r.entry(t, r.b)
	if !eb.Trusted || eb.TrustedAt == "" || r.entry(t, r.a).TrustedAt != "" {
		t.Fatalf("B %+v A %+v", eb, r.entry(t, r.a))
	}
}

// Fix 5: an older scan never replaces a newer one.
func TestAnOlderScanNeverOverwritesANewerOne(t *testing.T) {
	root := projectRepo(t, repoHelperTOML)
	ts := &projectdots.TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	var mu sync.Mutex
	block := false // the next scan-read blocks (once)
	entered, release := make(chan struct{}), make(chan struct{})
	r := newRigFull(t, func(d *Deps) { projectDeps(d, ts, root); noPoll(d) }, func(e *Engine) {
		e.testHook = func(point string) {
			mu.Lock()
			b := block && point == "scan-read"
			if b {
				block = false
			}
			mu.Unlock()
			if b {
				close(entered)
				<-release
			}
		}
	})
	_ = r.e.ScanPane(context.Background(), pane)
	pr := memory.Project{ID: repoProject, Name: "app", Root: root}
	mu.Lock()
	block = true
	mu.Unlock()
	done := make(chan struct{})
	go func() { r.e.scanRoot(pr); close(done) }() // reads the old file, then waits
	select {
	case <-entered:
	case <-done:
		t.Fatal("scanRoot has no scan-read point between its read and its install")
	}
	editHelper(t, root, repoHelperTOML, "Be helpful.", "Be newer.")
	r.e.scanRoot(pr) // a newer scan installs first
	close(release)
	<-done
	want := projectdots.Hex(projectdots.Sum([]byte(strings.Replace(repoHelperTOML, "Be helpful.", "Be newer.", 1))))
	for _, d := range r.e.Snapshot().Library {
		if d.Slug == helperSlug && d.SHA256 != want {
			t.Fatalf("the stale scan replaced the newer one: %s", d.SHA256)
		}
	}
}

// Fix 7: untrust revokes the stored entry even when the file is gone or no
// longer parses.
func TestUntrustWorksForADeletedOrBrokenFile(t *testing.T) {
	for name, change := range map[string]func(root string){
		"deleted": func(root string) { os.Remove(filepath.Join(root, filepath.FromSlash(helperRel))) },
		"broken": func(root string) {
			os.WriteFile(filepath.Join(root, filepath.FromSlash(helperRel)), []byte("name = = 1"), 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newProjRig(t)
			_ = r.e.ScanPane(context.Background(), pane)
			r.trustAs(t)
			change(r.root)
			if err := r.e.Untrust(context.Background(), pane, helperSlug); err != nil {
				t.Fatal(err)
			}
			if es, _ := r.trust.List(); len(es) != 0 {
				t.Fatalf("entries left: %+v", es)
			}
		})
	}
}

// Fix 8: an MCP ask of a trusted project dot with mcp granted runs the
// clamped copy; list_dots marks the dots the MCP server can't ask.
func TestMCPAskOfAGrantedProjectDotRunsClamped(t *testing.T) {
	r := newProjRig(t, dot("Security", dots.ModeQueue))
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t, dots.CapMCP) // inject not granted
	r.attach(t, helperSlug)
	if _, err := r.e.Ask(context.Background(), pane, helperSlug, "is this safe?", false); err != nil {
		t.Fatal(err)
	}
	if req := r.be.last(); req.Purpose != "Be helpful." {
		t.Fatalf("request %+v", req)
	}
	ld, err := r.e.ListDots(pane)
	if err != nil || len(ld.Dots) != 1 || ld.Dots[0].Mode != "queue" || ld.Dots[0].Status == dotUnavailable {
		t.Fatalf("list_dots %+v %v", ld, err)
	}
}

func TestListDotsMarksDotsTheMCPServerCantAsk(t *testing.T) {
	r := newProjRig(t, dot("Security", dots.ModeQueue))
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t) // mcp not granted
	r.attach(t, "security", helperSlug)
	ld, _ := r.e.ListDots(pane)
	for _, d := range ld.Dots {
		switch d.Slug {
		case "security":
			if d.Status == dotUnavailable {
				t.Fatalf("security is askable: %+v", d)
			}
		case helperSlug:
			if d.Status != dotUnavailable || d.PurposeSummary != "" {
				t.Fatalf("helper is not askable: %+v", d)
			}
		}
	}
	if _, err := r.e.askable(pane, helperSlug); err == nil || !strings.Contains(err.Error(), "proj:helper") || strings.Contains(err.Error(), helperSlug) {
		t.Fatalf("errors name project dots by their display slug: %v", err)
	}
}

// Fix 9: a closed pane's repository is forgotten, and so is one only looked
// at (ScanPane) once that is old.
func TestProjectsOfClosedPanesArePruned(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.attach(t, helperSlug)
	r.e.dropPane(pane)
	if _, ok := r.libDot(helperSlug); ok {
		t.Fatal("a closed pane's project dots are dropped")
	}
	r.e.mu.Lock()
	n, m := len(r.e.projects), len(r.e.paneRoots)
	r.e.mu.Unlock()
	if n != 0 || m != 0 {
		t.Fatalf("%d projects and %d pane roots left", n, m)
	}
}

func TestALookedAtRepositoryIsPrunedWhenOld(t *testing.T) {
	clk := &proClock{t: proStart}
	r := newProjRigWith(t, func(d *Deps) { d.Clock = clk.Now })
	_ = r.e.ScanPane(context.Background(), pane) // the manage popup, nothing attached
	time.Sleep(60 * time.Millisecond)            // polls
	if _, ok := r.libDot(helperSlug); !ok {
		t.Fatal("a fresh look stays while the popup is open")
	}
	clk.Add(time.Hour)
	eventually(t, "pruned", func() bool { _, ok := r.libDot(helperSlug); return !ok })
}

// Fix 9: turning project dots on at runtime finds attached panes'
// repositories without a re-attach.
func TestTurningProjectDotsOnFindsAttachedPanes(t *testing.T) {
	r := newProjRigWith(t, func(d *Deps) { d.Config.ProjectDotsEnabled = false }, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	if _, ok := r.libDot(helperSlug); ok {
		t.Fatal("project dots are off")
	}
	on := r.e.config()
	on.ProjectDotsEnabled = true
	r.e.cfg.Store(&on)
	eventually(t, "the attached pane's repository is scanned", func() bool {
		_, ok := r.libDot(helperSlug)
		return ok && r.e.Snapshot().PaneProjects[pane] == repoProject
	})
}
