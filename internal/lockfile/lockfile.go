// Package lockfile guarantees one engine per Herdr session with an OS file
// lock: flock(2) on Unix, LockFileEx on Windows. The OS releases the lock if
// the holder dies, so stale locks can't happen.
package lockfile

import (
	"errors"
	"fmt"
	"os"
)

var ErrLocked = errors.New("lock held by another process")

type Lock struct{ f *os.File }

// TryLock takes the exclusive lock on path without waiting, creating the file
// if needed, and records this process's pid in it. A lock held through any
// other open of the file, in this process or another, is ErrLocked.
func TryLock(path string) (*Lock, error) {
	f, err := openLockFile(path)
	if err != nil {
		return nil, err
	}
	// Never lock or truncate anything but a regular file (a symlink, a
	// reparse point, a device): the pid write would land on its target.
	if st, serr := f.Stat(); serr != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("lock file %s is not a regular file; refusing to use it", path)
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return &Lock{f: f}, nil
}

// Stat describes the locked file itself (through the open handle), so a
// caller can check that its path still names that file (os.SameFile).
func (l *Lock) Stat() (os.FileInfo, error) { return l.f.Stat() }

func (l *Lock) Release() error {
	_ = unlockFile(l.f)
	return l.f.Close()
}

// Held reports whether another holder currently owns the lock.
func Held(path string) bool {
	l, err := TryLock(path)
	if errors.Is(err, ErrLocked) {
		return true
	}
	if err == nil {
		_ = l.Release()
	}
	return false
}
