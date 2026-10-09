package memory

import (
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

var ev = Evidence{Session: "s1", Pane: "w1:p1", Turn: 4}

func TestApplyAddsDotActiveAndProjectProposed(t *testing.T) {
	s := testStore(t)
	res, err := s.Apply(pid, "security", []backend.MemoryOp{
		{Op: "add", Kind: "note", Text: "The team prefers zod over hand-written validators."},
		{Op: "add", Scope: "project", Kind: "convention", Text: "Components live in src/ui.", Paths: []string{"src/ui/**"}},
	}, ev)
	if err != nil || res.Added != 2 || len(res.Rejected) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	d := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	pr := list(t, s, pid, Filter{Scope: ScopeProject})
	if len(d) != 1 || d[0].Status != StatusActive || d[0].Source != SourceModel || d[0].Evidence != ev {
		t.Fatalf("dot %+v", d)
	}
	if len(pr) != 1 || pr[0].Status != StatusProposed || pr[0].Dot != "security" || pr[0].Paths[0] != "src/ui/**" {
		t.Fatalf("project %+v", pr)
	}
	s.Limits.AutoApproveProject = true
	s.Apply(pid, "security", []backend.MemoryOp{{Op: "add", Scope: "project", Kind: "decision", Text: "We deploy with fly.io."}}, ev)
	for _, e := range list(t, s, pid, Filter{Scope: ScopeProject}) {
		if e.Text == "We deploy with fly.io." && e.Status != StatusActive {
			t.Fatal("memory_auto_approve_project must make project ops active")
		}
	}
}

func TestApplyRefusesWhatModelsMayNotWrite(t *testing.T) {
	s := testStore(t)
	user := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "developer note", Source: SourceUser})
	other := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "frontend", Kind: KindNote, Text: "frontend note", Source: SourceModel})
	approved := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Dot: "security", Kind: KindConvention, Text: "approved convention", Source: SourceModel})
	res, err := s.Apply(pid, "security", []backend.MemoryOp{
		{Op: "add", Kind: "dismissed", Text: "stop raising CSRF"},
		{Op: "add", Kind: "preference", Text: "likes tabs"},
		{Op: "add", Scope: "global", Text: "global fact"},
		{Op: "add", Text: "the staging key is sk-live-" + strings.Repeat("a", 30) + " AKIAIOSFODNN7EXAMPLE"},
		{Op: "add", Text: strings.Repeat("x", 501)},
		{Op: "update", ID: user.Ref().String(), Text: "hijacked"},
		{Op: "forget", ID: "g:m1"},
		{Op: "update", ID: approved.Ref().String(), Text: "rewritten convention"},
		{Op: "forget", ID: "d:m99"},
		{Op: "shout", Text: "x"},
	}, ev)
	if err != nil || res.Applied() != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if len(res.Rejected) != backend.MaxPassOps {
		t.Fatalf("Apply caps ops at %d; rejected %v", backend.MaxPassOps, res.Rejected)
	}
	for _, why := range res.Rejected {
		if strings.Contains(why, "AKIA") || strings.Contains(why, "sk-live") {
			t.Fatalf("rejection leaks the secret: %q", why)
		}
	}
	res2, _ := s.Apply(pid, "security", []backend.MemoryOp{
		{Op: "update", ID: user.Ref().String(), Text: "hijacked"},
		{Op: "forget", ID: "g:m1"},
		{Op: "update", ID: approved.Ref().String(), Text: "rewritten convention"},
		{Op: "forget", ID: "d:m99"},
		{Op: "shout", Text: "x"},
	}, ev)
	if res2.Applied() != 0 || len(res2.Rejected) != 5 {
		t.Fatalf("%+v", res2)
	}
	if got, _ := s.Get(pid, user.Ref()); got.Text != "developer note" {
		t.Fatal("a model edited a developer entry")
	}
	if got, _ := s.Get(pid, approved.Ref()); got.Text != "approved convention" {
		t.Fatal("a model edited an approved project entry")
	}
	if got, _ := s.Get(pid, other.Ref()); got.Text != "frontend note" {
		t.Fatal("a model edited another dot's entry")
	}
}

func TestApplyUpdatesAndForgetsOwnEntries(t *testing.T) {
	s := testStore(t)
	s.Apply(pid, "security", []backend.MemoryOp{
		{Op: "add", Text: "Sessions expire after one hour."},
		{Op: "add", Scope: "project", Kind: "convention", Text: "Use zod for validation."},
	}, ev)
	res, err := s.Apply(pid, "security", []backend.MemoryOp{
		{Op: "update", ID: "d:m1", Text: "Sessions expire after thirty minutes."},
		{Op: "forget", ID: "p:m1"},
	}, ev)
	if err != nil || res.Updated != 1 || res.Forgotten != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if got, _ := s.Get(pid, Ref{Scope: ScopeDot, Dot: "security", ID: "m1"}); got.Text != "Sessions expire after thirty minutes." {
		t.Fatalf("%+v", got)
	}
	if n := len(list(t, s, pid, Filter{Scope: ScopeProject})); n != 0 {
		t.Fatalf("own proposal not forgotten: %d", n)
	}
	res, _ = s.Apply(pid, "security", []backend.MemoryOp{{Op: "add", Text: "sessions expire after THIRTY minutes"}}, ev)
	if res.Updated != 1 || res.Added != 0 {
		t.Fatalf("a duplicate add counts as an update: %+v", res)
	}
}

// A control character inside a secret must not hide it from the scan: the
// scan runs on the text that gets stored.
func TestApplyScansWhatItStores(t *testing.T) {
	s := testStore(t)
	own := mustAdd(t, s, pid, dotNote("security", "an own note"))
	res, err := s.Apply(pid, "security", []backend.MemoryOp{
		{Op: "add", Text: "the key is AKIA\u0007IOSFODNN7EXAMPLE ok"},
		{Op: "update", ID: own.Ref().String(), Text: "the key is AKIA\u0007IOSFODNN7EXAMPLE ok"},
		{Op: "add", Scope: "SECRET-SCOPE", Text: "x"},
		{Op: "add", Kind: "SECRET-KIND", Text: "x"},
		{Op: "forget", ID: "SECRET-ID"},
	}, ev)
	if err != nil || res.Applied() != 0 || len(res.Rejected) != 5 {
		t.Fatalf("%+v %v", res, err)
	}
	for _, why := range res.Rejected {
		if strings.Contains(why, "SECRET") {
			t.Fatalf("rejection echoes model text: %q", why)
		}
	}
	for _, e := range list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}) {
		if strings.Contains(e.Text, "AKIA") {
			t.Fatalf("secret stored: %+v", e)
		}
	}
}
