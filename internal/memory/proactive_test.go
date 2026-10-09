package memory

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/backend"
)

func TestResearchKindsOnlyInResearchPasses(t *testing.T) {
	s := testStore(t)
	ops := []backend.MemoryOp{
		{Op: "add", Kind: "question", Text: "Is the session cookie rotated on privilege change?"},
		{Op: "add", Kind: "research", Text: "lodash 4.17.20 has a prototype pollution advisory.", Paths: []string{"package.json"}},
	}
	res, err := s.Apply(pid, "security", ops, ev)
	if err != nil || res.Applied() != 0 || len(res.Rejected) != 2 {
		t.Fatalf("a review may not write research kinds: %+v %v", res, err)
	}
	res, err = s.Apply(pid, "security", ops, ev, ApplyOptions{Research: true})
	if err != nil || res.Added != 2 {
		t.Fatalf("a research pass may: %+v %v", res, err)
	}
	got := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Kinds: []Kind{KindQuestion}})
	if len(got) != 1 || got[0].Status != StatusActive || got[0].Source != SourceModel {
		t.Fatalf("question %+v", got)
	}
	// The other model-op rules still hold in a research pass.
	res, _ = s.Apply(pid, "security", []backend.MemoryOp{
		{Op: "add", Kind: "dismissed", Text: "stop raising CSRF"},
		{Op: "add", Scope: "global", Kind: "research", Text: "global fact"},
		{Op: "add", Scope: "project", Kind: "research", Text: "The API uses JWT access tokens with 15 minute expiry."},
		{Op: "add", Kind: "research", Text: "token ghp_" + strings.Repeat("a", 36)},
	}, ev, ApplyOptions{Research: true})
	if res.Added != 1 || len(res.Rejected) != 3 {
		t.Fatalf("%+v", res)
	}
	if pr := list(t, s, pid, Filter{Scope: ScopeProject, Kinds: []Kind{KindResearch}}); len(pr) != 1 || pr[0].Status != StatusProposed {
		t.Fatalf("project research stays proposed until approved: %+v", pr)
	}
}

func TestRenderListsResearchAndQuestions(t *testing.T) {
	out := Render([]Entry{
		ent("m1", ScopeDot, "security", KindQuestion, "Is the cookie rotated?"),
		ent("m2", ScopeDot, "security", KindResearch, "No advisory for jose 5.2."),
		ent("m3", ScopeDot, "security", KindNote, "plain note"),
	})
	q, r, n := strings.Index(out, "Open questions you noted to look into:"), strings.Index(out, "Your research notes:"), strings.Index(out, "Notes:")
	if q < 0 || r < 0 || n < 0 || !(r < q && q < n) || !strings.Contains(out, "- [d:m1] Is the cookie rotated?") {
		t.Fatalf("render:\n%s", out)
	}
}

func TestClaimScheduleRunsASlotOnce(t *testing.T) {
	s := testStore(t)
	slot := time.Date(2026, 10, 21, 9, 0, 0, 0, time.FixedZone("CDT", -5*3600))
	if last, err := s.LastSchedule(pid, "security"); err != nil || !last.IsZero() {
		t.Fatalf("no claims yet: %v %v", last, err)
	}
	if ok, err := s.ClaimSchedule(pid, "security", slot); !ok || err != nil {
		t.Fatalf("first claim: %v %v", ok, err)
	}
	if ok, _ := s.ClaimSchedule(pid, "security", slot); ok {
		t.Fatal("the same slot must not be claimed twice")
	}
	if ok, _ := s.ClaimSchedule(pid, "security", slot.Add(-24*time.Hour)); ok {
		t.Fatal("an older slot must not be claimed after a newer one")
	}
	if ok, _ := s.ClaimSchedule(pid, "frontend", slot); !ok {
		t.Fatal("each dot has its own slots")
	}
	if last, _ := s.LastSchedule(pid, "security"); !last.Equal(slot) {
		t.Fatalf("last %v", last)
	}
	raw, _ := os.ReadFile(filepath.Join(s.Dir, pid, "schedule.json"))
	if !strings.Contains(string(raw), `"security": "2026-10-21T09:00:00-05:00"`) {
		t.Fatalf("file %s", raw)
	}
	if _, err := s.ClaimSchedule("../etc", "security", slot); err == nil {
		t.Fatal("invalid project id")
	}
}

// Separate processes (the test binary re-run) racing for one slot: exactly
// one wins. Goroutines would share the in-process state and prove less.
func TestClaimScheduleConcurrentClaimsOneWinner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	const procs = 6
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*bytes.Buffer, procs)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestClaimScheduleChild$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "MEMORY_CLAIM_DIR="+dir)
		outs[i] = &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = outs[i], outs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	wins := 0
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("claimer %d: %v\n%s", i, err, outs[i])
		}
		if strings.Contains(outs[i].String(), "CLAIM-WON") {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d winners, want 1", wins)
	}
	if fi, err := os.Stat(filepath.Join(dir, pid, "schedule.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("schedule.json mode: %v %v", fi, err)
	}
}

// TestClaimScheduleChild is the claimer process; it does nothing in a normal run.
func TestClaimScheduleChild(t *testing.T) {
	dir := os.Getenv("MEMORY_CLAIM_DIR")
	if dir == "" {
		t.Skip("child of TestClaimScheduleConcurrentClaimsOneWinner")
	}
	lim := DefaultLimits()
	lim.LockTimeout = 10 * time.Second
	ok, err := Open(dir, lim).ClaimSchedule(pid, "security", time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		os.Stdout.WriteString("CLAIM-WON\n")
	}
}

func TestClaimScheduleReplacesACorruptFile(t *testing.T) {
	s := testStore(t)
	os.MkdirAll(filepath.Join(s.Dir, pid), 0o700)
	os.WriteFile(filepath.Join(s.Dir, pid, "schedule.json"), []byte("{oops"), 0o600)
	if ok, err := s.ClaimSchedule(pid, "security", time.Now()); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
}

func TestSeeDotsAndRecentDotProjects(t *testing.T) {
	s := testStore(t)
	other := "g-fedcba9876543210"
	for _, pr := range []Project{{ID: pid, Name: "herdr-dots", Root: "/src/herdr-dots"}, {ID: other, Name: "old", Root: "/src/old"}} {
		if err := s.SeeProject(pr); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	if err := s.SeeDots(pid, []string{"security", "Bad Slug"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SeeDots(other, []string{"security"}, now.Add(-20*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecentDotProjects("security", now.Add(-14*24*time.Hour))
	if err != nil || len(got) != 1 || got[0].ID != pid || got[0].Roots[0] != "/src/herdr-dots" {
		t.Fatalf("%+v %v", got, err)
	}
	// SeeDots may run before SeeProject (both are spawned on attach).
	fresh := "g-0000000000000001"
	if err := s.SeeDots(fresh, []string{"security"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SeeProject(Project{ID: fresh, Name: "fresh", Root: "/src/fresh"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.RecentDotProjects("security", now.Add(-time.Hour))
	if len(got) != 2 || got[0].Name != "fresh" || got[0].Roots[0] != "/src/fresh" {
		t.Fatalf("either order works: %+v", got)
	}
	if got, _ := s.RecentDotProjects("frontend", now.Add(-14*24*time.Hour)); len(got) != 0 {
		t.Fatalf("frontend was never attached: %+v", got)
	}
	// SeeProject keeps the record.
	_ = s.SeeProject(Project{ID: pid, Name: "herdr-dots", Root: "/other/checkout"})
	if got, _ := s.RecentDotProjects("security", now.Add(-time.Hour)); len(got) != 2 {
		t.Fatalf("SeeProject dropped dots_seen: %+v", got)
	}
}

func TestResearchKindsRefusedByMemoryPass(t *testing.T) {
	s := testStore(t)
	// Both the default path and an explicit zero-value option refuse them.
	ops := []backend.MemoryOp{{Op: "add", Kind: "research", Text: "x finding"}, {Op: "add", Kind: "question", Text: "why?"}}
	for _, o := range [][]ApplyOptions{nil, {{}}} {
		res, err := s.Apply(pid, "security", ops, ev, o...)
		if err != nil || res.Applied() != 0 || len(res.Rejected) != 2 {
			t.Fatalf("%+v %v", res, err)
		}
	}
}

func TestResearchKindsAreValidButNotModelKinds(t *testing.T) {
	for _, k := range ResearchKinds {
		if !k.Valid() || slices.Contains(ModelKinds, k) {
			t.Fatalf("%s: valid=%v", k, k.Valid())
		}
	}
}

func TestDeleteDotScrubsResearchAndProactiveState(t *testing.T) {
	s := testStore(t)
	ops := []backend.MemoryOp{{Op: "add", Kind: "research", Text: "lodash advisory"}, {Op: "add", Kind: "question", Text: "rotate cookie?"}}
	if _, err := s.Apply(pid, "security", ops, ev, ApplyOptions{Research: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	_ = s.SeeDots(pid, []string{"security", "frontend"}, now)
	_, _ = s.ClaimSchedule(pid, "security", now)
	_, _ = s.ClaimSchedule(pid, "frontend", now)
	if _, err := s.DeleteDot("security"); err != nil {
		t.Fatal(err)
	}
	if got := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Kinds: ResearchKinds}); len(got) != 0 {
		t.Fatalf("research survived: %+v", got)
	}
	if last, _ := s.LastSchedule(pid, "security"); !last.IsZero() {
		t.Fatalf("claim survived: %v", last)
	}
	if last, _ := s.LastSchedule(pid, "frontend"); !last.Equal(now) {
		t.Fatalf("other dot's claim lost: %v", last)
	}
	if got, _ := s.RecentDotProjects("security", now.Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("dots_seen survived: %+v", got)
	}
	if got, _ := s.RecentDotProjects("frontend", now.Add(-time.Hour)); len(got) != 1 {
		t.Fatalf("other dot's dots_seen lost: %+v", got)
	}
}
