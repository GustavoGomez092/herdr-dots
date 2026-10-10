package memory

import (
	"slices"
	"testing"
	"time"
)

// M15: entries with equal scores are ordered by scope, dot and id, not by
// the order the store returned them, so the block is the same bytes every
// time (prompt caching). Every entry is still selected.
func TestSelectBreaksScoreTiesByScopeDotAndID(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(sc Scope, dot, id, text string) Entry {
		return Entry{Scope: sc, Dot: dot, ID: id, Kind: KindNote, Status: StatusActive, Source: SourceModel, Text: text, CreatedAt: at, UpdatedAt: at}
	}
	es := []Entry{
		mk(ScopeDot, "security", "m10", "dot ten"),
		mk(ScopeGlobal, "security", "m1", "global one"),
		mk(ScopeDot, "security", "m2", "dot two"),
		mk(ScopeProject, "", "m3", "project three"),
	}
	q := Query{Dot: "security", Now: at}
	want := Render(Select(es, q, 4000))
	rev := slices.Clone(es)
	slices.Reverse(rev)
	if got := Render(Select(rev, q, 4000)); got != want {
		t.Fatalf("order depends on the input:\n%s\n---\n%s", want, got)
	}
	sel := Select(es, q, 4000)
	if len(sel) != 4 {
		t.Fatalf("selected %d of 4", len(sel))
	}
	var refs []string
	for _, e := range sel {
		refs = append(refs, e.Ref().String())
	}
	if got := refs; !slices.Equal(got, []string{"p:m3", "d:m2", "d:m10", "g:m1"}) {
		t.Fatalf("order %v", got)
	}
}
