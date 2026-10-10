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
	fallbackEnv(t, shortTemp(t), "")
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
