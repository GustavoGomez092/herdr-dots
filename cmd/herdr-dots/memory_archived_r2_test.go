package main

import (
	"fmt"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

// Round 2: pinning an entry that was already archived is not a cap action.
func TestMemoryPinOfAnAlreadyArchivedEntryDoesNotBlameTheCap(t *testing.T) {
	c, out, _ := newMemCLI(t)
	e, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "shared", Kind: memory.KindNote, Text: "Old note", Source: memory.SourceUser})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.store.Update(testPID, e.Ref(), func(x *memory.Entry) error { x.Status = memory.StatusArchived; return nil }); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	mustRun(t, c, "pin", e.Ref().String(), "--dot", "shared")
	if got := out.String(); got != "pinned "+e.Ref().String()+"\n" {
		t.Fatalf("got %q", got)
	}
}

// Round 2: research the cap archives is not an import; the message says so.
func TestMemoryUnpinOfArchivedResearchSaysResearchGivesWay(t *testing.T) {
	c, out, _ := newMemCLI(t)
	c.store.Limits.MaxDot = 3
	for i := range 2 {
		if _, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "shared", Kind: memory.KindNote,
			Text: fmt.Sprintf("word%dx kilo%dy lima%dz", i, i, i), Source: memory.SourceUser}); err != nil {
			t.Fatal(err)
		}
	}
	e, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "shared", Kind: memory.KindResearch,
		Text: "Research on caching", Source: memory.SourceModel, Status: memory.StatusActive, Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	c.store.Limits.MaxDot = 2
	out.Reset()
	mustRun(t, c, "unpin", e.Ref().String(), "--dot", "shared")
	want := e.Ref().String() + " was archived at once: shared's memory is full (limit 2); research notes give way first. Forget an entry or raise the limit."
	if !strings.Contains(out.String(), want) || strings.Contains(out.String(), "import") {
		t.Fatalf("want %q in:\n%s", want, out)
	}
}
