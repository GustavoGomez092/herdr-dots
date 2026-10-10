// Package fsutil holds small filesystem helpers shared by the state stores.
package fsutil

import (
	"os"
	"path/filepath"
)

// restrictFn is a test seam for restrict.
var restrictFn = restrict

// syncDirFn is a test seam for syncDir.
var syncDirFn = syncDir

// syncDir fsyncs a directory so a rename in it survives a power loss. It is
// best-effort: the renamed file is already complete and synced, so a failure
// here can only lose the rename (the old file stays), never corrupt either;
// Windows refuses FlushFileBuffers on a read-only directory handle, and some
// filesystems can't sync a directory.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// WriteFileAtomic writes data to a temp file in the same directory and renames
// it over path, so readers never observe a half-written file. The parent
// directory is created with 0700. A private perm (no group or other bits)
// also gets an owner-only DACL on Windows, applied to the temp file before any
// byte is written, and a write whose privacy can't be set fails. On Windows
// the rename is retried briefly while another process has the old file open.
// After the rename the parent directory is synced (best-effort, syncDir).
// A file a newer herdr-dots wrote (see ProtectNewer) is never replaced.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := CheckWritable(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once renamed
	if err := restrictFn(tmp, perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := rename(tmp, path); err != nil {
		return err
	}
	syncDirFn(dir)
	return nil
}
