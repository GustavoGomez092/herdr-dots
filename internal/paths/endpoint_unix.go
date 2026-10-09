//go:build unix

package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// EngineEndpoint is where the engine serves IPC: the unix socket EngineSock.
func (p Paths) EngineEndpoint() string { return p.EngineSock() }

func (p Paths) ensureEndpoint() error { return p.ensureSockFallback() }

// maxSockPath stays under the 104-byte sun_path limit on macOS.
const maxSockPath = 100

// sockFallbackDir returns a per-uid directory in the temp dir for socket fallback.
// This ensures the directory is private to the user and protects against squatting.
func (p Paths) sockFallbackDir() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("herdr-dots-%d", os.Getuid()))
}

// EngineSock is the engine's unix socket. Deep state dirs would exceed the
// unix socket path limit, so those fall back to a short path in the temp dir.
// Use EngineEndpoint, which is this on Unix and a named pipe on Windows.
func (p Paths) EngineSock() string {
	s := filepath.Join(p.SessionDir, "dots.sock")
	if len(s) <= maxSockPath {
		return s
	}
	return filepath.Join(p.sockFallbackDir(), SessionKey(p.SocketPath)+".sock")
}

// ensureSockFallback creates the per-user temp directory when the primary
// socket path is too long (Unix only; see EngineSock).
func (p Paths) ensureSockFallback() error {
	primarySock := filepath.Join(p.SessionDir, "dots.sock")
	if len(primarySock) <= maxSockPath {
		return nil
	}
	fallbackDir := p.sockFallbackDir()
	if err := os.MkdirAll(fallbackDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(fallbackDir, 0o700); err != nil {
		return err
	}
	// Verify that even the fallback path stays within the limit
	fallbackSock := filepath.Join(fallbackDir, SessionKey(p.SocketPath)+".sock")
	if len(fallbackSock) > maxSockPath {
		return fmt.Errorf("engine socket path too long: %s", fallbackSock)
	}
	return nil
}
