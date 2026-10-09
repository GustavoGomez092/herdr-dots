//go:build windows

package credentials

import (
	"io/fs"
	"os"
)

// openChecked refuses a symlink or other reparse point at path (Lstat), then
// opens it and returns the handle's own FileInfo. Windows has no O_NOFOLLOW;
// the gap between the Lstat and the open needs write access to CONFIG_DIR,
// which is private to the user (owner-only DACL), and read checks the
// privacy of the handle it reads (platform.CheckPrivateFile).
func openChecked(path string) (*os.File, fs.FileInfo, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, st, nil
	}
	fd, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	fst, err := fd.Stat()
	if err != nil {
		fd.Close()
		return nil, nil, err
	}
	return fd, fst, nil
}
