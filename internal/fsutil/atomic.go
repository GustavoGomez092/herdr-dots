// Package fsutil holds small filesystem helpers shared by the state stores.
package fsutil

import (
	"os"
	"path/filepath"
)

// restrictFn is a test seam for restrict.
var restrictFn = restrict

// WriteFileAtomic writes data to a temp file in the same directory and renames
// it over path, so readers never observe a half-written file. The parent
// directory is created with 0700. A private perm (no group or other bits)
// also gets an owner-only DACL on Windows, applied to the temp file before any
// byte is written, and a write whose privacy can't be set fails. On Windows
// the rename is retried briefly while another process has the old file open.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
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
	return rename(tmp, path)
}
