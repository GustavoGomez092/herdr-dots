//go:build !darwin && !linux

package credentials

import (
	"io/fs"
	"os"
)

// openChecked keeps the Lstat-then-open behavior on platforms without
// O_NOFOLLOW (Phase 6 ports Windows).
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
	return fd, st, nil
}

func ownedByMe(fs.FileInfo) bool { return true }
