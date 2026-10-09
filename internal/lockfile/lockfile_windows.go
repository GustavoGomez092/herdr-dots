//go:build windows

package lockfile

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffsetHigh places the one locked byte at 1<<62, far past the pid the
// file holds, so the lock never blocks reading or rewriting the content.
const lockOffsetHigh = 0x40000000

// openLockFile opens path with FILE_SHARE_DELETE, so a directory holding a
// lock file can still be removed (memory Purge) while the lock is held.
func openLockFile(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func lockFile(f *os.File) error {
	ol := &windows.Overlapped{OffsetHigh: lockOffsetHigh}
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	ol := &windows.Overlapped{OffsetHigh: lockOffsetHigh}
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
