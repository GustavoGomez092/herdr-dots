// Package platformtest holds test assertions whose meaning differs by OS, so
// tests in other packages state intent ("this file is private") instead of
// Unix mode bits.
package platformtest

import (
	"path/filepath"
	"testing"
)

// Abs joins parts under an absolute root: /a/b on Unix, C:\a\b on Windows.
// Use it where a test needs an absolute path that never touches the disk.
func Abs(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }

// Endpoint is a fresh engine IPC endpoint for a test, never served before:
// a short unix socket path (macOS caps them at 104 bytes) whose directory is
// removed after the test, or a unique named pipe on Windows. Serve it with
// ipc.Listen and dial it with ipc.Dial or an ipc.Client.
func Endpoint(t testing.TB) string {
	t.Helper()
	return endpoint(t)
}

// AssertPrivateFile fails t unless path is a file only its owner can use:
// mode 0600 on Unix, an owner-only DACL on Windows.
func AssertPrivateFile(t testing.TB, path string) {
	t.Helper()
	assertPrivate(t, path, false)
}

// AssertPrivateDir fails t unless path is a directory only its owner can
// use: mode 0700 on Unix, an owner-only DACL on Windows.
func AssertPrivateDir(t testing.TB, path string) {
	t.Helper()
	assertPrivate(t, path, true)
}
