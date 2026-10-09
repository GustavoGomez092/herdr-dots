package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/backend"
)

// fillSuppressors adds n sent/dismissed outcome entries (the ones that stop
// repeats) to the given scope and returns their texts.
func fillSuppressors(t *testing.T, s *Store, sc Scope, n int) {
	t.Helper()
	for i := range n {
		k := KindDismissed
		if i%2 == 1 {
			k = KindFinding
		}
		mustAdd(t, s, pid, Entry{Scope: sc, Dot: dotFor(sc, "security"), Kind: k, Text: distinct(i), Source: SourceOutcome, Status: StatusActive})
	}
}

func addResearch(t *testing.T, s *Store, sc Scope, base, n int) {
	t.Helper()
	for i := range n {
		kind := "research"
		if i%2 == 1 {
			kind = "question"
		}
		op := backend.MemoryOp{Op: "add", Kind: kind, Text: distinct(base + i)}
		if sc == ScopeProject {
			op.Scope = "project"
		}
		if _, err := s.Apply(pid, "security", []backend.MemoryOp{op}, ev, ApplyOptions{Research: true}); err != nil {
			t.Fatal(err)
		}
	}
}

func archivedSuppressors(t *testing.T, s *Store, sc Scope) []Entry {
	t.Helper()
	var out []Entry
	for _, e := range list(t, s, pid, Filter{Scope: sc, Dot: dotFor(sc, "security"), Statuses: []Status{StatusArchived}}) {
		if e.Kind == KindDismissed || e.Kind == KindFinding {
			out = append(out, e)
		}
	}
	return out
}

// Phase 2 is vital: fresh research may never archive the entries that stop
// repeats, even when the scope file is at its cap.
func TestResearchNeverEvictsSuppressingEntries(t *testing.T) {
	for _, sc := range []Scope{ScopeDot, ScopeProject} {
		t.Run(string(sc), func(t *testing.T) {
			s := testStore(t)
			s.Now = clock()
			s.Limits.MaxDot, s.Limits.MaxProject = 10, 10
			s.Limits.AutoApproveProject = true
			fillSuppressors(t, s, sc, 10)
			addResearch(t, s, sc, 100, 20)
			if got := archivedSuppressors(t, s, sc); len(got) != 0 {
				t.Fatalf("research archived %d suppressing entries: %+v", len(got), got)
			}
			live := list(t, s, pid, Filter{Scope: sc, Dot: dotFor(sc, "security"), Kinds: []Kind{KindDismissed, KindFinding}})
			if len(live) != 10 {
				t.Fatalf("%d suppressing entries live, want 10", len(live))
			}
		})
	}
}

// With room in research candidates, the oldest research goes first.
func TestResearchIsEvictedBeforeOtherKinds(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxDot = 10
	fillSuppressors(t, s, ScopeDot, 4)
	addResearch(t, s, ScopeDot, 100, 6)
	mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: distinct(500), Source: SourceModel})
	if got := archivedSuppressors(t, s, ScopeDot); len(got) != 0 {
		t.Fatalf("archived %+v", got)
	}
	arch := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Statuses: []Status{StatusArchived}})
	if len(arch) != 1 || (arch[0].Kind != KindResearch && arch[0].Kind != KindQuestion) {
		t.Fatalf("want one research entry archived, got %+v", arch)
	}
}

func TestScrubDotProactiveRefusesANewerMeta(t *testing.T) {
	s := testStore(t)
	dir := filepath.Join(s.Dir, pid)
	os.MkdirAll(dir, 0o700)
	raw := `{"version":99,"id":"` + pid + `","dots_seen":{"security":"2026-10-21T09:00:00Z"}}`
	os.WriteFile(filepath.Join(dir, "meta.json"), []byte(raw), 0o600)
	if _, err := s.DeleteDot("security"); !errors.Is(err, ErrVersion) {
		t.Fatalf("want ErrVersion, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "meta.json")); string(got) != raw {
		t.Fatalf("a newer meta.json was rewritten: %s", got)
	}
}

func TestDeleteDotReplacesACorruptSchedule(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	mustAdd(t, s, pid, dotNote("security", "a note"))
	dir := filepath.Join(s.Dir, pid)
	os.WriteFile(filepath.Join(dir, "schedule.json"), []byte("{oops"), 0o600)
	if _, err := s.DeleteDot("security"); err != nil {
		t.Fatalf("a corrupt schedule must not block forgetting: %v", err)
	}
	if ok, err := s.ClaimSchedule(pid, "security", now, now); !ok || err != nil {
		t.Fatalf("a recreated dot inherited a stale claim: %v %v", ok, err)
	}
	if got := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestDeleteDotWithCorruptMetaStillRemovesTheDot(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "a note"))
	if err := s.RecordOutcome(pid, Outcome{Dot: "security", Card: "1", Kind: OutcomeSent, Body: "Add rate limiting."}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(s.Dir, pid, "meta.json"), []byte("{oops"), 0o600)
	_, err := s.DeleteDot("security")
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
	if got := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(got) != 0 {
		t.Fatalf("dot file survived: %+v", got)
	}
	if led, _ := s.Ledger(pid); len(led) != 0 {
		t.Fatalf("ledger survived: %+v", led)
	}
}

func TestDeleteDotRemovesMetaAsidesThatMentionTheDot(t *testing.T) {
	s := testStore(t)
	dir := filepath.Join(s.Dir, pid)
	os.MkdirAll(dir, 0o700)
	mention := filepath.Join(dir, "meta.json.corrupt-1")
	other := filepath.Join(dir, "meta.json.corrupt-2")
	os.WriteFile(mention, []byte(`{"dots_seen":{"security": "2026-10`), 0o600)
	os.WriteFile(other, []byte(`{"dots_seen":{"frontend": "2026-10`), 0o600)
	if _, err := s.DeleteDot("security"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mention); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("aside naming the dot survived: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("an unrelated aside was removed: %v", err)
	}
}

func TestUpdateFromANonResearchPassCannotRewriteResearch(t *testing.T) {
	s := testStore(t)
	res, err := s.Apply(pid, "security", []backend.MemoryOp{
		{Op: "add", Kind: "research", Text: "original research"},
		{Op: "add", Kind: "question", Text: "original question"},
	}, ev, ApplyOptions{Research: true})
	if err != nil || res.Added != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security", Kinds: ResearchKinds})
	for _, e := range es {
		r, err := s.Apply(pid, "security", []backend.MemoryOp{{Op: "update", ID: e.Ref().String(), Text: "rewritten"}}, ev)
		if err != nil || r.Applied() != 0 || len(r.Rejected) != 1 {
			t.Fatalf("update of %s from a non-research pass: %+v %v", e.Kind, r, err)
		}
		got, _ := s.Get(pid, e.Ref())
		if strings.Contains(got.Text, "rewritten") {
			t.Fatalf("rewritten: %+v", got)
		}
		r, _ = s.Apply(pid, "security", []backend.MemoryOp{{Op: "update", ID: e.Ref().String(), Text: "rewritten"}}, ev, ApplyOptions{Research: true})
		if r.Updated != 1 {
			t.Fatalf("a research pass may update: %+v", r)
		}
	}
	// forget stays allowed in any pass.
	r, _ := s.Apply(pid, "security", []backend.MemoryOp{{Op: "forget", ID: es[0].Ref().String()}}, ev)
	if r.Forgotten != 1 {
		t.Fatalf("forget: %+v", r)
	}
}
