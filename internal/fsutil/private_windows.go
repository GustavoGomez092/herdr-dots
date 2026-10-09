//go:build windows

package fsutil

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"

	"herdr-dots/internal/platform"
)

// restrict gives a file written with a private perm an owner-only DACL;
// Windows ignores Unix permission bits.
func restrict(path string, perm os.FileMode) error {
	if perm&0o077 != 0 {
		return nil
	}
	return platform.SecureFile(path)
}

// renameTries x renameWait bounds how long rename waits for a reader (Go
// opens files without FILE_SHARE_DELETE, which blocks replacing them).
const (
	renameTries = 40
	renameWait  = 25 * time.Millisecond
)

func rename(from, to string) error {
	var err error
	for i := 0; i < renameTries; i++ {
		if err = os.Rename(from, to); err == nil || !busy(err) {
			return err
		}
		time.Sleep(renameWait)
	}
	return err
}

// busy reports errors worth retrying. ERROR_ACCESS_DENIED is included because
// that is what a rename onto a file another process has open returns, so a
// genuinely denied write is also retried for about a second (40 x 25 ms)
// before the error surfaces.
func busy(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
