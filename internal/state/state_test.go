package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttachmentsCRUDAndPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "attachments.json")
	s, err := OpenAttachments(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Put(Attachment{PaneID: "w1:p2", Agent: "codex"})
	_ = s.Put(Attachment{PaneID: "w1:p1", Agent: "claude", Dots: []DotAttachment{{Slug: "security"}}})
	if l := s.List(); len(l) != 2 || l[0].PaneID != "w1:p1" {
		t.Fatalf("list %+v", l)
	}
	err = s.Update("w1:p1", func(a *Attachment) { a.Find("security").LastSeenTurn = 7 })
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update("nope", func(*Attachment) {}); !errors.Is(err, ErrNoAttachment) {
		t.Fatalf("want ErrNoAttachment, got %v", err)
	}
	got, _ := s.Get("w1:p1")
	got.Dots[0].LastSeenTurn = 99 // mutating a copy must not leak into the store
	again, _ := s.Get("w1:p1")
	if again.Dots[0].LastSeenTurn != 7 {
		t.Fatalf("Get must return a deep copy, got %d", again.Dots[0].LastSeenTurn)
	}
	s2, _ := OpenAttachments(p)
	if a, ok := s2.Get("w1:p1"); !ok || a.Find("security").LastSeenTurn != 7 {
		t.Fatal("not persisted")
	}
	_ = s2.Delete("w1:p1")
	if _, ok := s2.Get("w1:p1"); ok {
		t.Fatal("delete")
	}
	if (&Attachment{}).Find("x") != nil {
		t.Fatal("Find on missing slug must be nil")
	}
}

func TestUsage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	u, _ := OpenUsage(p)
	_ = u.Record("p1", "security", 100, 10)
	_ = u.Record("p1", "security", 50, 5)
	_ = u.Record("p1", "frontend", 1, 1)
	if u.Calls("p1", "security") != 2 || u.PaneCalls("p1") != 3 || u.Calls("p2", "x") != 0 {
		t.Fatal("counts")
	}
	u2, _ := OpenUsage(p)
	if u2.PaneCalls("p1") != 3 {
		t.Fatal("not persisted")
	}
	_ = u2.ResetPane("p1")
	if u2.PaneCalls("p1") != 0 {
		t.Fatal("reset")
	}
}

func TestFeedAppendRecentAndReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "feed.jsonl")
	f, err := OpenFeed(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 60; i++ {
		_ = f.Append(FeedEntry{Time: time.Now(), PaneID: "p1", Dot: "security", Turn: i, Outcome: OutcomePass})
	}
	_ = f.Append(FeedEntry{PaneID: "p2", Dot: "x", Outcome: OutcomeError, Detail: "boom"})
	r := f.Recent("p1", 5)
	if len(r) != 5 || r[4].Turn != 60 || r[0].Turn != 56 {
		t.Fatalf("recent %+v", r)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
	f2, _ := OpenFeed(p)
	if r := f2.Recent("p1", 100); len(r) != 50 || r[49].Turn != 60 {
		t.Fatalf("reload keeps last 50 per pane, got %d", len(r))
	}
	if r := f2.Recent("p2", 10); len(r) != 1 || r[0].Detail != "boom" {
		t.Fatal(fmt.Sprint("p2 ", r))
	}
}
