package queue

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestOnlyAPatchCardCanBeApplied(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "q.json"))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := s.Add(Card{Dot: "a", PaneID: "p", Body: "x"})
	if err := s.SetStatus(plain.ID, StatusApplied); !errors.Is(err, ErrTransition) {
		t.Fatalf("SetStatus applied on a plain card: %v", err)
	}
	if err := s.SetStatusNote(plain.ID, StatusApplied, "n"); !errors.Is(err, ErrTransition) {
		t.Fatalf("SetStatusNote applied on a plain card: %v", err)
	}
	patch, _ := s.Add(Card{Kind: KindPatch, PatchID: "0123456789abcdef", Dot: "a", PaneID: "p", Body: "x"})
	if err := s.Fail(patch.ID, "delivery failed"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatusNote(patch.ID, StatusApplied, "applied"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(patch.ID); got.Status != StatusApplied || got.Error != "" {
		t.Fatalf("applied card %+v: the delivery error must be cleared", got)
	}
}
