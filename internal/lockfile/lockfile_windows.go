//go:build windows

package lockfile

import (
	"errors"
	"os"
	"unsafe"

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

// fileAttributeTagInfo is FILE_ATTRIBUTE_TAG_INFO.
type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

// plainLockFile reports whether the open lock file (opened with
// FILE_FLAG_OPEN_REPARSE_POINT, so a link is the link itself) is a file on
// disk that names no other path. Go's Stat calls every reparse point but a
// symlink irregular, which would refuse OneDrive cloud files under a synced
// profile (triage W7); only name surrogates are refused here.
func plainLockFile(f *os.File) bool {
	h := windows.Handle(f.Fd())
	typ, err := windows.GetFileType(h)
	if err != nil {
		return false
	}
	var ti fileAttributeTagInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&ti)), uint32(unsafe.Sizeof(ti))); err != nil {
		return false
	}
	return usableLockFile(ti.FileAttributes, ti.ReparseTag, typ)
}

// usableLockFile: a disk file, not a directory, and no name-surrogate
// reparse point (IO_REPARSE_TAG_SYMLINK, IO_REPARSE_TAG_MOUNT_POINT for
// junctions, any tag with the name-surrogate bit). Other reparse tags (cloud
// files, deduplicated files) hold their own data and are accepted.
func usableLockFile(attrs, tag, fileType uint32) bool {
	const nameSurrogate = 0x20000000 // IsReparseTagNameSurrogate
	switch {
	case fileType != windows.FILE_TYPE_DISK:
		return false
	case attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0:
		return false
	case attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 && tag&nameSurrogate != 0:
		return false
	}
	return true
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
