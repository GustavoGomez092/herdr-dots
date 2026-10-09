package main

import (
	"fmt"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

// Phase 9 residual (P9-R15): when approving or unpinning an import takes a
// full scope over its limit the import is archived at once; the CLI says so.

// fullShared fills the "shared" dot to limit with the developer's own
// entries and returns a pending imported entry.
func fullShared(t *testing.T, c *memCLI, limit int) memory.Entry {
	t.Helper()
	c.store.Limits.MaxDot = limit
	for i := range limit {
		if _, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "shared", Kind: memory.KindNote,
			Text: fmt.Sprintf("word%dx kilo%dy lima%dz", i, i, i), Source: memory.SourceUser}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.store.ImportPack(testPID, "shared", []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "Imported advice about caching"}}); err != nil {
		t.Fatal(err)
	}
	es, _ := c.store.List(testPID, memory.Filter{Scope: memory.ScopeDot, Dot: "shared", Statuses: []memory.Status{memory.StatusProposed}})
	if len(es) != 1 {
		t.Fatalf("%+v", es)
	}
	return es[0]
}

// pinnedImport adds an approved, pinned import to "shared" and then lowers
// the limit so that unpinning it takes the scope over.
func pinnedImport(t *testing.T, c *memCLI) memory.Entry {
	t.Helper()
	c.store.Limits.MaxDot = 3
	for i := range 2 {
		if _, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "shared", Kind: memory.KindNote,
			Text: fmt.Sprintf("word%dx kilo%dy lima%dz", i, i, i), Source: memory.SourceUser}); err != nil {
			t.Fatal(err)
		}
	}
	e, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "shared", Kind: memory.KindNote,
		Text: "Imported pinned advice", Source: memory.SourceImport, Status: memory.StatusActive, Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	c.store.Limits.MaxDot = 2
	return e
}

func wantArchivedMsg(t *testing.T, out, verb string, e memory.Entry) {
	t.Helper()
	want := fmt.Sprintf("%s %s, but it was archived at once: shared's memory is full (limit 2) and imported entries give way to yours. Forget an entry or raise the limit, then import it again.", verb, e.Ref())
	if !strings.Contains(out, want) {
		t.Fatalf("want %q in:\n%s", want, out)
	}
	if strings.Contains(out, "pin it") || strings.Contains(strings.ToLower(out), "pinning") {
		t.Fatalf("must not suggest pinning:\n%s", out)
	}
	if strings.Contains(out, verb+" "+e.Ref().String()+"\n") {
		t.Fatalf("also printed the plain success line:\n%s", out)
	}
}

func TestMemoryApproveSaysWhenTheEntryWasArchivedAtOnce(t *testing.T) {
	c, out, _ := newMemCLI(t)
	e := fullShared(t, c, 2)
	out.Reset()
	mustRun(t, c, "approve", e.Ref().String(), "--yes", "--dot", "shared")
	wantArchivedMsg(t, out.String(), "approved", e)
}

func TestMemoryUnpinSaysWhenTheEntryWasArchivedAtOnce(t *testing.T) {
	c, out, _ := newMemCLI(t)
	e := pinnedImport(t, c)
	out.Reset()
	mustRun(t, c, "unpin", e.Ref().String(), "--dot", "shared")
	wantArchivedMsg(t, out.String(), "unpinned", e)
}
