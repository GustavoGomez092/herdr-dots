//go:build windows

package credentials

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"herdr-dots/internal/platform"
)

// openChecked opens path without following a reparse point and refuses a
// symlink, junction or mount point on the handle itself
// (platform.OpenNoFollow: FILE_FLAG_OPEN_REPARSE_POINT, so there is no
// check-to-open gap). The handle's final path must be the config dir's final
// path plus the file name (platform.VerifyOpenedPath). It returns the
// handle's own FileInfo; read then checks the privacy of the handle it reads
// (platform.CheckPrivateFile).
func openChecked(path string) (*os.File, fs.FileInfo, error) {
	fd, err := platform.OpenNoFollow(path)
	if errors.Is(err, platform.ErrReparsePoint) {
		return nil, nil, fmt.Errorf("credentials.toml must be a regular file (%s)", path)
	}
	if err != nil {
		return nil, nil, err
	}
	if err := platform.VerifyOpenedPath(fd, filepath.Dir(path), filepath.Base(path)); err != nil {
		fd.Close()
		return nil, nil, fmt.Errorf("credentials.toml must be a regular file (%s)", path)
	}
	fst, err := fd.Stat()
	if err != nil {
		fd.Close()
		return nil, nil, err
	}
	return fd, fst, nil
}
