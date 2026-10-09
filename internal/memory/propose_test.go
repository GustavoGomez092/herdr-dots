package memory

import (
	"errors"
	"strings"
	"testing"
)

func TestProposeForcesAProjectProposalFromTheAgent(t *testing.T) {
	s := testStore(t)
	got, err := s.Propose(pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindConvention, Text: "Use pnpm, never npm.",
		Paths: []string{"package.json"}, Status: StatusActive, Source: SourceUser, Pinned: true, Evidence: Evidence{Pane: "w1:p1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != ScopeProject || got.Status != StatusProposed || got.Source != SourceAgent || got.Dot != "" || got.Pinned ||
		got.Kind != KindConvention || got.Evidence.Pane != "w1:p1" || got.Ref().String() != "p:"+got.ID {
		t.Fatalf("stored %+v", got)
	}
	es := list(t, s, pid, Filter{Scope: ScopeProject, Statuses: []Status{StatusProposed}})
	if len(es) != 1 || es[0].Text != "Use pnpm, never npm." || es[0].Paths[0] != "package.json" {
		t.Fatalf("listed %+v", es)
	}
}

func TestProposeRefusesSecretsAndStoresNothing(t *testing.T) {
	s := testStore(t)
	_, err := s.Propose(pid, Entry{Kind: KindNote, Text: "the deploy key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"})
	if !errors.Is(err, ErrSecret) || !strings.Contains(err.Error(), "anthropic key") {
		t.Fatalf("got %v", err)
	}
	if es := list(t, s, pid, Filter{}); len(es) != 0 {
		t.Fatalf("stored %+v", es)
	}
}

func TestProposeRefusesOtherKindsAndBadProjects(t *testing.T) {
	s := testStore(t)
	for _, k := range []Kind{KindFinding, KindDismissed, KindPreference, ""} {
		if _, err := s.Propose(pid, Entry{Kind: k, Text: "x"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("kind %q: got %v", k, err)
		}
	}
	if _, err := s.Propose("../..", Entry{Kind: KindNote, Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad project: %v", err)
	}
	if _, err := s.Propose(pid, Entry{Kind: KindNote, Text: strings.Repeat("a", 501)}); !errors.Is(err, ErrTooLong) {
		t.Fatalf("too long: %v", err)
	}
}

func TestProposeMatchingAnApprovedEntryKeepsItApproved(t *testing.T) {
	s := testStore(t)
	approved := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindDecision, Text: "State lives in zustand stores."})
	got, err := s.Propose(pid, Entry{Kind: KindDecision, Text: "State lives in zustand stores."})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != approved.ID || got.Status != StatusActive || got.Source != SourceUser || got.Uses != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestAgentIsAValidSource(t *testing.T) {
	if !SourceAgent.Valid() {
		t.Fatal("imports and hand edits must accept source \"agent\"")
	}
}

func TestProposeScansAfterSanitizing(t *testing.T) {
	s := testStore(t)
	for _, text := range []string{
		"key sk-ant-\x00api03-abcdefghijklmnopqrstuvwxyz012345",
		"key sk-ant-\x1bapi03-abcdefghijklmnopqrstuvwxyz012345",
	} {
		if _, err := s.Propose(pid, Entry{Kind: KindNote, Text: text}); !errors.Is(err, ErrSecret) {
			t.Errorf("%q: got %v", text, err)
		}
	}
	if es := list(t, s, pid, Filter{}); len(es) != 0 {
		t.Fatalf("stored %+v", es)
	}
}

func TestProposeScansPathsAndEvidenceWithoutEchoingThem(t *testing.T) {
	s := testStore(t)
	key := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"
	for _, e := range []Entry{
		{Kind: KindNote, Text: "x", Paths: []string{"**/" + key}},
		{Kind: KindNote, Text: "x", Paths: []string{"src/AKIAIOSFODNN7EXAMPLE/**"}},
		{Kind: KindNote, Text: "x", Paths: []string{"a/sk-ant-\x00api03-abcdefghijklmnopqrstuvwxyz012345"}},
		{Kind: KindNote, Text: "x", Evidence: Evidence{Card: key}},
	} {
		_, err := s.Propose(pid, e)
		if !errors.Is(err, ErrSecret) {
			t.Errorf("%+v: got %v", e, err)
			continue
		}
		if strings.Contains(err.Error(), "abcdefghij") || strings.Contains(err.Error(), "AKIAIOSFODNN7") {
			t.Errorf("error echoes the value: %v", err)
		}
	}
	if es := list(t, s, pid, Filter{}); len(es) != 0 {
		t.Fatalf("stored %+v", es)
	}
}

func TestProposeTextBoundary(t *testing.T) {
	s := testStore(t)
	if _, err := s.Propose(pid, Entry{Kind: KindNote, Text: strings.Repeat("é", 500)}); err != nil {
		t.Fatalf("500 runes: %v", err)
	}
	if _, err := s.Propose(pid, Entry{Kind: KindNote, Text: strings.Repeat("é", 501)}); !errors.Is(err, ErrTooLong) {
		t.Fatalf("501 runes: %v", err)
	}
}

func TestProposeNeverRewordsASimilarEntry(t *testing.T) {
	s := testStore(t)
	dev := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindConvention, Text: "Run the linter before every commit in this repo",
		Paths: []string{"src/**"}})
	got, err := s.Propose(pid, Entry{Kind: KindConvention, Text: "Run the linter before every commit in this repo please",
		Paths: []string{"other/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != dev.ID || got.Text != dev.Text || got.Source != SourceUser || got.Status != StatusActive ||
		len(got.Paths) != 1 || got.Paths[0] != "src/**" || got.Uses != 1 {
		t.Fatalf("developer entry changed: %+v", got)
	}
}

func TestProposeKeepsTheFirstAgentWording(t *testing.T) {
	s := testStore(t)
	first, err := s.Propose(pid, Entry{Kind: KindConvention, Text: "Run the linter before every commit in this repo", Paths: []string{"src/**"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Propose(pid, Entry{Kind: KindConvention, Text: "Run the linter before every commit in this repo please", Paths: []string{"other/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID || got.Text != first.Text || len(got.Paths) != 1 || got.Uses != 1 || got.Status != StatusProposed {
		t.Fatalf("agent reworded its own proposal: %+v", got)
	}
}

func TestAuthorNamesTheMainAgent(t *testing.T) {
	if got := author(Entry{Source: SourceAgent, Status: StatusProposed}); got != "the main agent, unapproved" {
		t.Fatalf("got %q", got)
	}
	if got := author(Entry{Source: SourceAgent, Status: StatusActive}); got != "the main agent" {
		t.Fatalf("got %q", got)
	}
}
