// Package lockfile guarantees one engine per Herdr session with flock(2). The
// kernel releases the lock if the engine dies, so stale locks can't happen.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var ErrLocked = errors.New("lock held by another process")

type Lock struct{ f *os.File }

func TryLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return &Lock{f: f}, nil
}

func (l *Lock) Release() error {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
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
