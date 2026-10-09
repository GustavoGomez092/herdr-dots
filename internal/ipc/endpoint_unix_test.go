//go:build unix

package ipc

import (
	"os"
	"path/filepath"
	"testing"
)

// testEndpoint is a fresh socket path short enough for sun_path.
func testEndpoint(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ipc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "d.sock")
}

func TestListenReplacesAStaleSocketAndIsPrivate(t *testing.T) {
	path := testEndpoint(t)
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 || st.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket %v %v", st, err)
	}
	ln.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("closing the listener must remove the socket: %v", err)
	}
}
