//go:build windows

package paths

import "herdr-dots/internal/platform"

// EngineEndpoint is where the engine serves IPC on Windows: the named pipe
// \\.\pipe\herdr-dots-<SessionKey>, one per Herdr session.
func (p Paths) EngineEndpoint() string {
	return platform.PipePrefix + "herdr-dots-" + SessionKey(p.SocketPath)
}

// ensureEndpoint has nothing to create: a pipe needs no directory.
func (p Paths) ensureEndpoint() error { return nil }
