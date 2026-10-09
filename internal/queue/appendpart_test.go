package queue

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func digestCard(t *testing.T, s *Store) Card {
	t.Helper()
	c, err := s.Add(Card{Kind: KindDigest, Dot: "away", PaneID: "w1:p1", Severity: "info", Body: "## Consider\n- a (Security)",
		Mode: "queue", Label: "1 dot · 11 min", Parts: []CardPart{{Dot: "security", DotName: "Security", Severity: "info", Body: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// AppendPart grows a pending, unsent digest card in one write: the parts, the
// rebuilt body and label, and the card's severity (the highest part's).
func TestAppendPartGrowsAPendingDigest(t *testing.T) {
	s, p := open(t)
	c := digestCard(t, s)
	got, err := s.AppendPart(c.ID, "## Must fix\n- b\x1b[31m (Frontend)\n## Consider\n- a (Security)", "2 dots · 12 min",
		CardPart{Dot: "frontend", DotName: "Front\x1b]0;x\x07end", Severity: "critical", Body: "b\x1b[31m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Parts) != 2 || got.Severity != "critical" || got.Label != "2 dots · 12 min" || !strings.HasPrefix(got.Body, "## Must fix") {
		t.Fatalf("%+v", got)
	}
	if strings.ContainsAny(got.Body+got.Parts[1].Body+got.Parts[1].DotName, "\x1b\x07") {
		t.Fatalf("unsanitized: %+v", got)
	}
	s2, _ := Open(p)
	if r, _ := s2.Get(c.ID); len(r.Parts) != 2 || r.Severity != "critical" {
		t.Fatalf("not persisted: %+v", r)
	}
	// A lower part never lowers the severity.
	got, _ = s.AppendPart(c.ID, got.Body, "3 dots · 12 min", CardPart{Dot: "perf", Severity: "info", Body: "c"})
	if got.Severity != "critical" || len(got.Parts) != 3 {
		t.Fatalf("%+v", got)
	}
}

// Only a pending, unapproved digest card grows: a card being sent, already
// sent, dismissed or orphaned, or any other kind, is left as it is.
func TestAppendPartRefusesCardsThatAreNotPendingDigests(t *testing.T) {
	s, _ := open(t)
	part := CardPart{Dot: "frontend", Severity: "warn", Body: "b"}
	approved := digestCard(t, s)
	_ = s.Approve(approved.ID)
	sent := digestCard(t, s)
	_ = s.SetStatus(sent.ID, StatusSent)
	dismissed := digestCard(t, s)
	_ = s.SetStatus(dismissed.ID, StatusDismissed)
	plain, _ := s.Add(Card{Dot: "security", PaneID: "w1:p1", Body: "x"})
	for _, id := range []string{approved.ID, sent.ID, dismissed.ID, plain.ID} {
		before, _ := s.Get(id)
		if _, err := s.AppendPart(id, "new", "new", part); !errors.Is(err, ErrTransition) {
			t.Errorf("card %s: %v, want ErrTransition", id, err)
		}
		if after, _ := s.Get(id); after.Body != before.Body || len(after.Parts) != len(before.Parts) {
			t.Errorf("card %s changed: %+v", id, after)
		}
	}
	if _, err := s.AppendPart("nope", "b", "l", part); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing card: %v", err)
	}
}

// A failed write leaves the card in memory as it was.
func TestAppendPartRollsBackOnAFailedWrite(t *testing.T) {
	s, p := open(t)
	c := digestCard(t, s)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0o700); err != nil { // the atomic rename onto a directory fails
		t.Fatal(err)
	}
	if _, err := s.AppendPart(c.ID, "new", "new", CardPart{Dot: "frontend", Body: "b"}); err == nil {
		t.Fatal("want a write error")
	}
	if got, _ := s.Get(c.ID); len(got.Parts) != 1 || got.Body != c.Body || got.Label != c.Label {
		t.Fatalf("in-memory card changed after a failed write: %+v", got)
	}
}
