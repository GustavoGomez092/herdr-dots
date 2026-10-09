package queue

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

func patchCard() Card {
	return Card{Kind: KindPatch, Dot: "security", DotName: "Security", Glyph: "◆", PaneID: "w1:p1", Severity: "info",
		Body: "Moved the JWT to an httpOnly cookie.", Mode: "queue", PatchID: "0123456789abcdef", DiffStat: "3 files +42 −7", PatchFlags: []string{"binary"}}
}

func TestPatchCardAppliesAndLeavesTheVisibleQueue(t *testing.T) {
	s, p := open(t)
	c, _ := s.Add(patchCard())
	if err := s.SetStatusNote(c.ID, StatusApplied, "applied with conflicts in 1 file: a.go", FlagConflicts); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(c.ID)
	if got.Status != StatusApplied || got.Note == "" || !slices.Equal(got.PatchFlags, []string{"binary", FlagConflicts}) {
		t.Fatalf("card %+v", got)
	}
	if len(s.Visible("w1:p1")) != 0 || len(s.Pending("w1:p1")) != 0 {
		t.Fatal("an applied card is terminal and hidden")
	}
	if err := s.SetStatus(c.ID, StatusSent); !errors.Is(err, ErrTransition) {
		t.Fatalf("applied → sent: %v", err)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s2.Get(c.ID); again.PatchID != "0123456789abcdef" || again.DiffStat != "3 files +42 −7" || again.Status != StatusApplied {
		t.Fatalf("reloaded %+v", again)
	}
}

func TestOnlyPendingCardsCanBeApplied(t *testing.T) {
	s, _ := open(t)
	c, _ := s.Add(patchCard())
	_ = s.SetStatus(c.ID, StatusDismissed)
	if err := s.SetStatusNote(c.ID, StatusApplied, ""); !errors.Is(err, ErrTransition) {
		t.Fatalf("dismissed → applied: %v", err)
	}
	o, _ := s.Add(patchCard())
	_, _ = s.OrphanPane("w1:p1")
	if err := s.SetStatus(o.ID, StatusApplied); !errors.Is(err, ErrTransition) {
		t.Fatalf("orphaned → applied: %v", err)
	}
	if err := s.SetStatus(o.ID, StatusDismissed); err != nil {
		t.Fatalf("an orphaned patch can still be discarded: %v", err)
	}
}

func TestAppliedCardsAreTrimmedLikeOtherTerminalCards(t *testing.T) {
	s, p := open(t)
	for range keepTerminal + 5 {
		c, _ := s.Add(patchCard())
		_ = s.SetStatus(c.ID, StatusApplied)
	}
	live, _ := s.Add(patchCard())
	if n := len(s.All()); n != keepTerminal+1 {
		t.Fatalf("%d cards kept, want %d terminal + 1 live", n, keepTerminal)
	}
	if _, ok := s.Get(live.ID); !ok {
		t.Fatal("the live card was trimmed")
	}
	raw, _ := os.ReadFile(p)
	if strings.Count(string(raw), `"patch_id"`) != keepTerminal+1 {
		t.Fatal("file does not match memory")
	}
}

func TestNonPatchCardsCarryNoPatchFields(t *testing.T) {
	s, p := open(t)
	_, _ = s.Add(Card{Dot: "security", PaneID: "w1:p1", Body: "x", Mode: "queue"})
	raw, _ := os.ReadFile(p)
	for _, k := range []string{"patch_id", "diff_stat", "patch_flags"} {
		if strings.Contains(string(raw), k) {
			t.Errorf("%s written for a plain card", k)
		}
	}
}
