package memory

import (
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/lockfile"
)

func setHook[T any](t *testing.T, hook *T, fn T) {
	t.Helper()
	old := *hook
	*hook = fn
	t.Cleanup(func() { *hook = old })
}

// Review Focus (Important): a writer that gets the project's lock between
// Purge's locked removal and the removal of the directory must never see its
// write succeed and then vanish. It either fails or writes a fresh project
// that Purge leaves alone.
func TestPurgeNeverDeletesAWriteThatSucceeded(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "before the purge"))
	var added Entry
	var addErr error
	ran := false
	setHook(t, &purgeGapHook, func() {
		ran = true
		added, addErr = s.Add(pid, dotNote("security", "written during the purge"))
	})
	if err := s.Purge(pid); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("the purge gap hook did not run")
	}
	es, _ := s.List(pid, Filter{Scope: ScopeDot, Dot: "security"})
	if addErr == nil {
		if len(es) != 1 || es[0].ID != added.ID || es[0].Text != "written during the purge" {
			t.Fatalf("the acknowledged write is gone or the purged entry is back: %+v", es)
		}
		return
	}
	if len(es) != 0 {
		t.Fatalf("a failed write left entries: %+v", es)
	}
}

// A read in the same gap must not bring the purged project back as an empty
// directory (Projects would list it).
func TestAReadDuringPurgeLeavesNoDirectory(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "before the purge"))
	setHook(t, &purgeGapHook, func() {
		if es, err := s.List(pid, Filter{Scope: ScopeDot, Dot: "security"}); err != nil || len(es) != 0 {
			t.Errorf("read during the purge: %+v %v", es, err)
		}
	})
	if err := s.Purge(pid); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, pid)); !os.IsNotExist(err) {
		ents, _ := os.ReadDir(filepath.Join(s.Dir, pid))
		t.Fatalf("project directory left behind: %v (%v)", err, ents)
	}
}

// A reader that recreated just the lock file in the gap leaves nothing
// behind; a lock someone holds is left alone.
func TestPurgeRemovesABareLockFileButNotAHeldOne(t *testing.T) {
	for _, held := range []bool{false, true} {
		s := testStore(t)
		mustAdd(t, s, pid, dotNote("security", "x"))
		dir := filepath.Join(s.Dir, pid)
		var l *lockfile.Lock
		setHook(t, &purgeGapHook, func() {
			var err error
			if l, err = lockfile.TryLock(filepath.Join(dir, "memory.lock")); err != nil {
				t.Fatal(err)
			}
			if !held {
				l.Release()
			}
		})
		if err := s.Purge(pid); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(dir)
		if held {
			l.Release()
			if err != nil {
				t.Fatalf("held lock: the directory went: %v", err)
			}
		} else if !os.IsNotExist(err) {
			t.Fatalf("bare lock: directory left: %v", err)
		}
	}
}

// A reader whose lock file was unlinked while it waited reads without
// recreating the project.
func TestAReadWhoseLockFileWasUnlinkedDoesNotRecreateIt(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "first"))
	dir := filepath.Join(s.Dir, pid)
	setHook(t, &lockAcquiredHook, func(path string) {
		// Purge, as seen by a reader that already waited on the lock.
		_ = os.RemoveAll(dir)
	})
	if es, err := s.List(pid, Filter{Scope: ScopeDot, Dot: "security"}); err != nil || len(es) != 0 {
		t.Fatalf("read: %+v %v", es, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the read recreated the project: %v", err)
	}
}

// A writer that was waiting on a lock file Purge has since unlinked holds a
// lock nobody else can see. withLock must notice (the path no longer names
// the locked file), take the lock again on the current file and only then
// write.
func TestWithLockRetriesWhenItsLockFileWasUnlinked(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "first"))
	calls := 0
	setHook(t, &lockAcquiredHook, func(path string) {
		calls++
		if calls == 1 {
			if err := os.Remove(path); err != nil {
				t.Errorf("remove lock: %v", err)
			}
		}
	})
	if _, err := s.Add(pid, dotNote("security", "second")); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("lock taken %d times, want 2 (a retry on the current lock file)", calls)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, pid, "memory.lock")); err != nil {
		t.Fatalf("lock file: %v", err)
	}
	setHook(t, &lockAcquiredHook, nil)
	if es := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"}); len(es) != 2 {
		t.Fatalf("entries %+v", es)
	}
}

// A lock file that keeps disappearing is not retried for ever.
func TestWithLockGivesUpAfterBoundedRetries(t *testing.T) {
	s := testStore(t)
	mustAdd(t, s, pid, dotNote("security", "first"))
	calls := 0
	setHook(t, &lockAcquiredHook, func(path string) {
		calls++
		_ = os.Remove(path)
	})
	if _, err := s.Add(pid, dotNote("security", "second")); err == nil {
		t.Fatal("want an error when the lock file never stays")
	}
	if calls < 2 || calls > 10 {
		t.Fatalf("lock taken %d times", calls)
	}
}
