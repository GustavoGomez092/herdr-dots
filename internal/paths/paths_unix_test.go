//go:build unix

package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineEndpointIsTheUnixSocket(t *testing.T) {
	p := New("/c", "/s", "/sock")
	if p.EngineEndpoint() != p.EngineSock() || p.EngineEndpoint() != filepath.Join(p.SessionDir, "dots.sock") {
		t.Fatalf("endpoint %q", p.EngineEndpoint())
	}
}

// Review Focus 1: macOS caps unix socket paths at 104 bytes.
func TestEngineSockFallsBackWhenPathTooLong(t *testing.T) {
	long := "/" + strings.Repeat("d", 120)
	p := New("/c", long, "/sock")
	s := p.EngineSock()
	if len(s) > 100 {
		t.Fatalf("socket path too long (%d): %s", len(s), s)
	}
	if !strings.HasPrefix(s, os.TempDir()) {
		t.Fatalf("expected temp-dir fallback, got %s", s)
	}
	// Verify fallback uses per-uid directory
	expectedFallbackDir := p.sockFallbackDir()
	if !strings.HasPrefix(s, expectedFallbackDir) {
		t.Fatalf("expected fallback in per-uid dir %s, got %s", expectedFallbackDir, s)
	}
	short := New("/c", "/s", "/sock").EngineSock()
	if short != filepath.Join(New("/c", "/s", "/sock").SessionDir, "dots.sock") {
		t.Fatalf("short path should stay in session dir: %s", short)
	}
}

func TestEnsureCreatesFallbackDirWhenNeeded(t *testing.T) {
	// Use a long state directory path that triggers the fallback
	root := t.TempDir()
	long := filepath.Join(root, strings.Repeat("d", 100))
	p := New(filepath.Join(root, "c"), long, "/sock")

	// Ensure the fallback directory doesn't exist yet
	fallbackDir := p.sockFallbackDir()
	t.Cleanup(func() {
		// Clean up the fallback directory after the test
		os.RemoveAll(fallbackDir)
	})

	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}

	// Verify fallback directory was created with 0700 permissions
	st, err := os.Stat(fallbackDir)
	if err != nil {
		t.Fatalf("fallback dir not created: %v", err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("fallback dir perm = %v, want 0700", st.Mode().Perm())
	}

	// Verify the engine socket path is still within the limit
	sock := p.EngineSock()
	if len(sock) > 100 {
		t.Fatalf("fallback socket path too long (%d): %s", len(sock), sock)
	}
}
