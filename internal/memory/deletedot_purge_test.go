package memory

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// M36: a project Purge removed while DeleteDot waited for its lock is not
// brought back as a bare directory holding only memory.lock. Other projects
// keep every entry they had.
func TestDeleteDotLeavesNoBareLockDirectoryAfterAConcurrentPurge(t *testing.T) {
	s := testStore(t)
	const other = "g-00000000000000bb"
	mustAdd(t, s, pid, dotNote("security", "purged with its project"))
	kept := mustAdd(t, s, other, Entry{Scope: ScopeDot, Dot: "frontend", Kind: KindNote, Text: "another dot in another project", Source: SourceUser})
	dir := filepath.Join(s.Dir, pid)
	purged := false
	setHook(t, &lockAcquiredHook, func(path string) {
		if !purged && filepath.Dir(path) == dir {
			purged = true
			_ = os.RemoveAll(dir) // Purge, as seen by a DeleteDot that already listed the project
		}
	})
	if _, err := s.DeleteDot("security"); err != nil {
		t.Fatal(err)
	}
	if !purged {
		t.Fatal("hook did not run")
	}
	setHook(t, &lockAcquiredHook, nil)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		ents, _ := os.ReadDir(dir)
		t.Fatalf("purged project came back: %v %v", err, ents)
	}
	got, err := s.Get(other, kept.Ref())
	if err != nil || !reflect.DeepEqual(got, kept) {
		t.Fatalf("other project's entry: %+v %v", got, err)
	}
}
