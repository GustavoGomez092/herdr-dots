//go:build windows

package herdrtest

import (
	"net"
	"os"

	"herdr-dots/internal/platform"
)

// Listen serves a Herdr-style socket at path the way Herdr does on Windows:
// a marker file at path and a named pipe at \\.\pipe\<path>.
func Listen(path string) (net.Listener, error) {
	if err := os.WriteFile(path, []byte("herdrtest\n"), 0o600); err != nil {
		return nil, err
	}
	return platform.ListenPipe(platform.PipeName(path))
}
