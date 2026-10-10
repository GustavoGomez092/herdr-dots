//go:build windows

package lockfile

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// Final review Minor 7: on Windows only name-surrogate reparse points
// (symlinks, junctions) are refused as lock files. Other reparse tags, such
// as OneDrive's cloud files under a synced profile, are ordinary files.
func TestLockFileAcceptsCloudFilesAndRefusesNameSurrogates(t *testing.T) {
	const (
		reparse    = windows.FILE_ATTRIBUTE_REPARSE_POINT
		cloud      = 0x9000001A // IO_REPARSE_TAG_CLOUD
		cloud6     = 0x9000601A // IO_REPARSE_TAG_CLOUD_6 (OneDrive Files On-Demand)
		dedup      = 0x80000013 // IO_REPARSE_TAG_DEDUP
		disk, pipe = windows.FILE_TYPE_DISK, windows.FILE_TYPE_PIPE
	)
	for _, c := range []struct {
		name            string
		attrs, tag, typ uint32
		ok              bool
	}{
		{"plain file", windows.FILE_ATTRIBUTE_NORMAL, 0, disk, true},
		{"archive bit", windows.FILE_ATTRIBUTE_ARCHIVE, 0, disk, true},
		{"OneDrive cloud file", windows.FILE_ATTRIBUTE_ARCHIVE | reparse, cloud6, disk, true},
		{"cloud placeholder", reparse, cloud, disk, true},
		{"deduplicated file", reparse, dedup, disk, true},
		{"symlink", reparse, windows.IO_REPARSE_TAG_SYMLINK, disk, false},
		{"junction", windows.FILE_ATTRIBUTE_DIRECTORY | reparse, windows.IO_REPARSE_TAG_MOUNT_POINT, disk, false},
		{"directory", windows.FILE_ATTRIBUTE_DIRECTORY, 0, disk, false},
		{"pipe", windows.FILE_ATTRIBUTE_NORMAL, 0, pipe, false},
	} {
		if got := usableLockFile(c.attrs, c.tag, c.typ); got != c.ok {
			t.Errorf("%s: usable = %v, want %v", c.name, got, c.ok)
		}
	}
}

// A plain lock file on disk is accepted through the real handle checks.
func TestPlainLockFileIsUsableOnWindows(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.lock")
	l, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
