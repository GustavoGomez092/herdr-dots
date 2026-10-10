package memory

import (
	"reflect"
	"testing"
	"time"
)

// M24: Prune never deletes a pinned entry, even an archived one, and an
// archived entry with a zero UpdatedAt is aged from its CreatedAt instead of
// from the zero time. No surviving entry is changed, relabelled or turned
// into something that suppresses.
func TestPruneNeverDeletesPinnedAndAgesZeroUpdatedAtFromCreatedAt(t *testing.T) {
	s := testStore(t)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := t0.Add(250 * 24 * time.Hour)
	s.Now = func() time.Time { return t0 }
	pinZero := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "pinned, archived, zero updated", Source: SourceModel, Pinned: true})
	pinOld := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindFinding, Text: "pinned, archived long ago", Source: SourceOutcome, Pinned: true})
	oldZero := mustAdd(t, s, pid, dotNote("security", "archived, zero updated, created long ago"))
	s.Now = func() time.Time { return recent }
	newZero := mustAdd(t, s, pid, dotNote("security", "archived, zero updated, created recently"))

	path := s.scopePath(pid, ScopeDot, "security")
	f, err := readScope(path, ScopeDot, "security")
	if err != nil {
		t.Fatal(err)
	}
	for i := range f.Entries {
		e := &f.Entries[i]
		e.Status = StatusArchived
		if e.ID == pinOld.ID {
			e.UpdatedAt = t0
		} else {
			e.UpdatedAt = time.Time{}
		}
	}
	if err := writeScope(path, f); err != nil {
		t.Fatal(err)
	}
	before := map[string]Entry{}
	for _, e := range f.Entries {
		before[e.ID] = e
	}

	rep, err := s.Prune(pid, t0.Add(291*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Deleted != 1 || rep.Archived != 0 {
		t.Fatalf("report %+v: want only the old unpinned entry deleted", rep)
	}
	after, err := readScope(path, ScopeDot, "security")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range after.Entries {
		got[e.ID] = e
	}
	for _, id := range []string{pinZero.ID, pinOld.ID, newZero.ID} {
		e, ok := got[id]
		if !ok {
			t.Fatalf("entry %s (%q) was deleted", id, before[id].Text)
		}
		if !reflect.DeepEqual(e, before[id]) {
			t.Fatalf("entry %s changed:\n got %+v\nwant %+v", id, e, before[id])
		}
		if e.Kind == KindDismissed {
			t.Fatalf("entry %s now suppresses", id)
		}
	}
	if _, ok := got[oldZero.ID]; ok {
		t.Fatalf("an unpinned archived entry created 291 days ago survived")
	}
	if len(got) != 3 {
		t.Fatalf("entries %+v", after.Entries)
	}
}
