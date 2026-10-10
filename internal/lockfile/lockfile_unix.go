//go:build unix

package lockfile

import (
	"errors"
	"os"
	"syscall"
)

func openLockFile(path string) (*os.File, error) {
	// O_NOFOLLOW: a symlinked lock file is refused, never written through.
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
}

// plainLockFile reports whether the open lock file is a regular file.
func plainLockFile(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode().IsRegular()
}

func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
