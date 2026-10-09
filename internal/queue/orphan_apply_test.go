package queue

import (
	"errors"
	"path/filepath"
	"testing"
)

// A pane can close while its patch is being applied: the patch lands, so the
// orphaned card must still be able to say so.
func TestAnOrphanedPatchCardCanBeMarkedApplied(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "q.json"))
	if err != nil {
		t.Fatal(err)
	}
	patch, _ := s.Add(Card{Kind: KindPatch, PatchID: "0123456789abcdef", Dot: "a", PaneID: "p", Body: "x"})
	plain, _ := s.Add(Card{Dot: "a", PaneID: "p", Body: "y"})
	if err := s.Fail(patch.ID, "delivery failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OrphanPane("p"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatusNote(patch.ID, StatusApplied, "applied to your working tree (not staged)"); err != nil {
		t.Fatalf("orphaned patch card → applied: %v", err)
	}
	if got, _ := s.Get(patch.ID); got.Status != StatusApplied || got.Error != "" {
		t.Fatalf("card %+v", got)
	}
	if err := s.SetStatus(plain.ID, StatusApplied); !errors.Is(err, ErrTransition) {
		t.Fatalf("an orphaned plain card can't be applied: %v", err)
	}
}
