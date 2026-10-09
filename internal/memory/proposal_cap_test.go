package memory

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// distinct is entry text that shares no token with distinct(j), j != i.
func distinct(i int) string { return fmt.Sprintf("word%dx kilo%dy lima%dz", i, i, i) }

// clock is a Store.Now that moves one minute per call.
func clock() func() time.Time {
	t := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Minute); return t }
}

func approve(t *testing.T, s *Store, e Entry) {
	t.Helper()
	if _, err := s.Update(pid, e.Ref(), func(x *Entry) error { x.Status = StatusActive; return nil }); err != nil {
		t.Fatal(err)
	}
}

// Phase 5 final review I2 (Phase 2 memory is vital): unapproved agent
// proposals never push approved memory out of a full project scope.
func TestProposalsNeverArchiveAnActiveEntry(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxProject = 10
	for i := range 9 {
		mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindConvention, Text: distinct(i), Source: SourceModel})
	}
	p, err := s.Propose(pid, Entry{Kind: KindConvention, Text: distinct(100)})
	if err != nil {
		t.Fatal(err)
	}
	approve(t, s, p) // an approved agent convention
	for i := range 12 {
		if _, err := s.Propose(pid, Entry{Kind: KindConvention, Text: distinct(200 + i)}); err != nil {
			t.Fatalf("proposal %d: %v", i+1, err)
		}
	}
	if es := list(t, s, pid, Filter{Scope: ScopeProject, Statuses: []Status{StatusActive}}); len(es) != 10 {
		t.Fatalf("%d active entries left, want all 10", len(es))
	}
	for _, e := range list(t, s, pid, Filter{Scope: ScopeProject, Statuses: []Status{StatusArchived}}) {
		if e.Source != SourceAgent || e.Text == distinct(100) {
			t.Fatalf("archived a non-proposal: %+v", e)
		}
	}
}

// The cap archives proposals before active entries, oldest proposal first.
func TestCapArchivesTheOldestProposalFirst(t *testing.T) {
	s := testStore(t)
	s.Now = clock()
	s.Limits.MaxProject = 3
	a := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindNote, Text: distinct(1), Source: SourceModel})
	p1, _ := s.Propose(pid, Entry{Kind: KindNote, Text: distinct(2)})
	p2, _ := s.Propose(pid, Entry{Kind: KindNote, Text: distinct(3)})
	b := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindNote, Text: distinct(4), Source: SourceModel})
	var archived, live []string
	for _, e := range list(t, s, pid, Filter{Scope: ScopeProject, Statuses: []Status{StatusActive, StatusProposed, StatusArchived}}) {
		if e.Status == StatusArchived {
			archived = append(archived, e.ID)
		} else {
			live = append(live, e.ID)
		}
	}
	if strings.Join(archived, ",") != p1.ID || len(live) != 3 {
		t.Fatalf("archived %v live %v; want only %s (oldest proposal) archived, %s %s %s live", archived, live, p1.ID, a.ID, p2.ID, b.ID)
	}
}

// Phase 5 final review I2: at most 20 agent proposals wait for the developer.
func TestTwentyFirstPendingAgentProposalIsRefused(t *testing.T) {
	s := testStore(t)
	var first Entry
	for i := range MaxPendingProposals {
		e, err := s.Propose(pid, Entry{Kind: KindNote, Text: distinct(i)})
		if err != nil {
			t.Fatalf("proposal %d: %v", i+1, err)
		}
		if i == 0 {
			first = e
		}
	}
	_, err := s.Propose(pid, Entry{Kind: KindNote, Text: distinct(500)})
	if !errors.Is(err, ErrTooManyProposals) || err.Error() != "20 proposals are waiting for the developer; approve or reject some first" {
		t.Fatalf("21st proposal: %v", err)
	}
	if n := len(list(t, s, pid, Filter{Scope: ScopeProject, Statuses: []Status{StatusProposed}})); n != 20 {
		t.Fatalf("%d proposals stored, want 20", n)
	}
	// Proposing a waiting text again stores nothing new, so it is allowed.
	if got, err := s.Propose(pid, Entry{Kind: KindNote, Text: distinct(3)}); err != nil || got.Status != StatusProposed {
		t.Fatalf("repeat of a waiting proposal: %+v %v", got, err)
	}
	// A dot's (model) proposals don't use the agent's slots.
	if _, err := s.Add(pid, Entry{Scope: ScopeProject, Kind: KindNote, Text: distinct(600), Source: SourceModel, Status: StatusProposed}); err != nil {
		t.Fatal(err)
	}
	approve(t, s, first) // approving one frees a slot
	if _, err := s.Propose(pid, Entry{Kind: KindNote, Text: distinct(500)}); err != nil {
		t.Fatalf("after approving one: %v", err)
	}
	if _, err := s.Propose(pid, Entry{Kind: KindNote, Text: distinct(501)}); !errors.Is(err, ErrTooManyProposals) {
		t.Fatalf("the freed slot is taken again: %v", err)
	}
}
