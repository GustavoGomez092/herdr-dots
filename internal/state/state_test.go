package state

import (
	"encoding/json"
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

func TestFeedRotatesLargeFileAtOpen(t *testing.T) {
	dir := t.TempDir()
	feedPath := filepath.Join(dir, "feed.jsonl")

	// Create a feed.jsonl file larger than feedRotateBytes (5 MiB)
	// Write valid JSONL entries until file exceeds 5 MiB
	file, err := os.Create(feedPath)
	if err != nil {
		t.Fatalf("create feed.jsonl: %v", err)
	}
	defer file.Close()

	targetSize := feedRotateBytes + 1000 // Slightly over 5 MiB
	entry := FeedEntry{
		Time:    time.Now(),
		PaneID:  "p1",
		Dot:     "test",
		Turn:    1,
		Outcome: OutcomePass,
		Detail:  string(make([]byte, 100)), // Padding to make lines larger
	}

	for {
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		line := append(data, '\n')
		n, err := file.Write(line)
		if err != nil {
			t.Fatalf("write to feed.jsonl: %v", err)
		}
		if n != len(line) {
			t.Fatalf("short write to feed.jsonl: wrote %d, expected %d", n, len(line))
		}

		stat, err := file.Stat()
		if err != nil {
			t.Fatalf("stat feed.jsonl: %v", err)
		}
		if stat.Size() > int64(targetSize) {
			break
		}
	}

	originalSize, err := file.Stat()
	if err != nil {
		t.Fatalf("final stat feed.jsonl: %v", err)
	}
	sizeBefore := originalSize.Size()
	file.Close()

	// Verify file exists and is large before rotation
	if sizeBefore <= int64(feedRotateBytes) {
		t.Fatalf("feed.jsonl too small before rotation: %d bytes, need > %d", sizeBefore, feedRotateBytes)
	}

	// Call OpenFeed - should trigger rotation
	f, err := OpenFeed(feedPath)
	if err != nil {
		t.Fatalf("OpenFeed: %v", err)
	}

	// Assert feed.jsonl.1 exists with original size
	rotatedPath := feedPath + ".1"
	rotatedStat, err := os.Stat(rotatedPath)
	if err != nil {
		t.Fatalf("stat feed.jsonl.1: %v", err)
	}
	if rotatedStat.Size() != sizeBefore {
		t.Fatalf("feed.jsonl.1 size mismatch: got %d, want %d", rotatedStat.Size(), sizeBefore)
	}

	// Assert feed.jsonl is new and empty (or very small after one operation)
	newFileStat, err := os.Stat(feedPath)
	if err != nil {
		// File may not exist if never written to after rotation - that's OK
		if !os.IsNotExist(err) {
			t.Fatalf("stat new feed.jsonl: %v", err)
		}
	} else if newFileStat.Size() != 0 {
		t.Fatalf("new feed.jsonl should be empty or not exist, got %d bytes", newFileStat.Size())
	}

	// Append a new entry to the rotated feed and verify Recent returns nothing
	// from the rotated file (only from new entries)
	err = f.Append(FeedEntry{Time: time.Now(), PaneID: "p2", Dot: "newdot", Turn: 1, Outcome: OutcomePass})
	if err != nil {
		t.Fatalf("Append after rotation: %v", err)
	}

	// Recent for p1 should return nothing (original entries were in rotated file)
	r := f.Recent("p1", 100)
	if len(r) != 0 {
		t.Fatalf("Recent for p1 should return empty after rotation, got %d entries", len(r))
	}

	// Recent for p2 should return the new entry
	r2 := f.Recent("p2", 100)
	if len(r2) != 1 || r2[0].Dot != "newdot" {
		t.Fatalf("Recent for p2 should return new entry, got %+v", r2)
	}
}
