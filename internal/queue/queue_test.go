package queue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/platform/platformtest"
)

func open(t *testing.T) (*Store, string) {
	p := filepath.Join(t.TempDir(), "queue.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestAddAssignsIDAndPersists(t *testing.T) {
	s, p := open(t)
	c, err := s.Add(Card{Dot: "security", PaneID: "w1:p1", Severity: "warn", Body: "x", Mode: "queue"})
	if err != nil || c.ID == "" || c.Status != StatusPending || c.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", c, err)
	}
	platformtest.AssertPrivateFile(t, p)
	s2, _ := Open(p)
	if got, ok := s2.Get(c.ID); !ok || got.Body != "x" {
		t.Fatal("not persisted")
	}
	c2, _ := s2.Add(Card{PaneID: "w1:p1", Body: "y"})
	if c2.ID == c.ID {
		t.Fatal("ids must stay unique across reopen")
	}
}

func TestPendingOrdersBySeverityThenAge(t *testing.T) {
	s, _ := open(t)
	a, _ := s.Add(Card{PaneID: "p", Severity: "info", Body: "a"})
	b, _ := s.Add(Card{PaneID: "p", Severity: "critical", Body: "b"})
	c, _ := s.Add(Card{PaneID: "p", Severity: "warn", Body: "c", Status: StatusAuto})
	_, _ = s.Add(Card{PaneID: "other", Severity: "critical", Body: "d"})
	got := s.Pending("p")
	if len(got) != 3 || got[0].ID != b.ID || got[1].ID != c.ID || got[2].ID != a.ID {
		t.Fatalf("order %+v", got)
	}
}

func TestTransitions(t *testing.T) {
	s, _ := open(t)
	c, _ := s.Add(Card{PaneID: "p", Body: "x"})
	if err := s.Approve(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(c.ID, StatusAuto); err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(c.ID, "boom"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(c.ID)
	if got.Status != StatusPending || got.Error != "boom" || !got.Approved {
		t.Fatalf("after fail %+v", got)
	}
	if err := s.SetStatus(c.ID, StatusSent); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(c.ID)
	if got.Error != "" {
		t.Fatal("sent clears error")
	}
	if err := s.SetStatus(c.ID, StatusPending); !errors.Is(err, ErrTransition) {
		t.Fatalf("sent is terminal, got %v", err)
	}
	if err := s.Edit(c.ID, "new"); !errors.Is(err, ErrTransition) {
		t.Fatalf("cannot edit sent card, got %v", err)
	}
	if err := s.SetStatus("nope", StatusSent); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestEditRejectsEmpty(t *testing.T) {
	s, _ := open(t)
	c, _ := s.Add(Card{PaneID: "p", Body: "x"})
	if err := s.Edit(c.ID, "   "); err == nil {
		t.Fatal("empty body must be rejected")
	}
	if err := s.Edit(c.ID, " better "); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(c.ID)
	if got.Body != "better" {
		t.Fatalf("body %q", got.Body)
	}
}

func TestOrphanPane(t *testing.T) {
	s, _ := open(t)
	a, _ := s.Add(Card{PaneID: "p", Body: "a"})
	b, _ := s.Add(Card{PaneID: "p", Body: "b"})
	_ = s.SetStatus(b.ID, StatusDismissed)
	n, err := s.OrphanPane("p")
	if err != nil || n != 1 {
		t.Fatalf("orphaned %d %v", n, err)
	}
	if len(s.Pending("p")) != 0 || len(s.Visible("p")) != 1 || s.Visible("p")[0].ID != a.ID {
		t.Fatal("orphaned cards are visible but not pending")
	}
	if err := s.SetStatus(a.ID, StatusDismissed); err != nil {
		t.Fatal("orphaned cards can be dismissed")
	}
}

func TestCouncilCardRoundTripsAndOldFilesLoad(t *testing.T) {
	s, p := open(t)
	c, err := s.Add(Card{Kind: KindCouncil, Dot: "council", DotName: "Council", Glyph: "⊕", PaneID: "w1:p1", Turn: 22,
		Severity: "critical", Body: "## Must fix\n- JWT in localStorage (Security)", Mode: "queue",
		Question: "Ready to merge?", Note: "merged without chair (chair error: timeout)",
		Parts: []CardPart{
			{Dot: "security", DotName: "Security", Glyph: "◆", Severity: "critical", Body: "JWT in localStorage", Stance: "object"},
			{Dot: "copy", DotName: "Copy", Glyph: "✎", Body: "timed out", Failed: true},
		}})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get(c.ID)
	if !ok || got.Kind != KindCouncil || got.Question != "Ready to merge?" || got.Note == "" || len(got.Parts) != 2 {
		t.Fatalf("council card after reopen: %+v", got)
	}
	if got.Parts[0].Stance != "object" || got.Parts[0].Severity != "critical" || !got.Parts[1].Failed || got.Parts[1].Body != "timed out" {
		t.Fatalf("parts %+v", got.Parts)
	}

	old := filepath.Join(t.TempDir(), "old.json")
	os.WriteFile(old, []byte(`{"next":1,"cards":[{"id":"1","dot":"security","dot_name":"Security","glyph":"◆","pane_id":"p","turn":3,"severity":"warn","body":"x","mode":"queue","approved":false,"status":"pending"}]}`), 0o600)
	s3, err := Open(old)
	if err != nil {
		t.Fatal(err)
	}
	if c3, ok := s3.Get("1"); !ok || c3.Kind != "" || c3.Parts != nil || c3.Body != "x" {
		t.Fatalf("a v1 queue.json must load unchanged: %+v", c3)
	}
}
