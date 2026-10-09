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
