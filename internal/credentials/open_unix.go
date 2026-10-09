//go:build unix

package credentials

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// openChecked opens path without following a symlink (O_NOFOLLOW) and without
// blocking on a FIFO, then returns the descriptor's own FileInfo, so the
// checks that follow apply to the file that will be read (no check/use gap).
func openChecked(path string) (*os.File, fs.FileInfo, error) {
	fd, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, nil, fmt.Errorf("credentials.toml must be a regular file (%s)", path)
		}
		return nil, nil, err
	}
	st, err := fd.Stat()
	if err != nil {
		fd.Close()
		return nil, nil, err
	}
	return fd, st, nil
}
