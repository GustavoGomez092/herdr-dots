package queue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
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
