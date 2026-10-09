//go:build unix

package platform

import (
	"fmt"
	"os"
	"syscall"
)

// SecureDir makes a directory private to the current user (0700).
func SecureDir(path string) error { return os.Chmod(path, 0o700) }

// SecureFile makes a file private to the current user (0600).
func SecureFile(path string) error { return os.Chmod(path, 0o600) }

// CheckPrivate fails with ErrNotPrivate when group or others have any
// permission bit on path, and with ErrNotOwner when another user owns it.
// Symlinks are not followed.
func CheckPrivate(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return checkPrivateInfo(st)
}

// CheckPrivateFile is CheckPrivate for a file already open: it checks the
// file the descriptor refers to (fstat), so a path swapped after the open
// can't pass the check.
func CheckPrivateFile(f *os.File) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return checkPrivateInfo(st)
}

func checkPrivateInfo(st os.FileInfo) error {
	if st.Mode().Perm()&0o077 != 0 {
		return ErrNotPrivate
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Getuid() {
		return ErrNotOwner
	}
	return nil
}

// FixPrivateHint tells the user how to make path private again.
func FixPrivateHint(path string) string { return fmt.Sprintf("run: chmod 600 %q", path) }

// OpenNoFollow opens path read-only and fails if its last element is a
// symlink. O_NONBLOCK keeps a FIFO from hanging the open; callers still
// require a regular file or a directory.
func OpenNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

// ReservedPathName reports whether a project-relative path names something
// other than a plain file. Unix has no such names.
func ReservedPathName(string) bool { return false }
