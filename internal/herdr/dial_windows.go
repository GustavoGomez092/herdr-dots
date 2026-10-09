//go:build windows

package herdr

import (
	"context"
	"net"

	"herdr-dots/internal/platform"
)

// dialSocket connects to the named pipe Herdr serves for HERDR_SOCKET_PATH
// (\\.\pipe\<path>). Herdr creates that pipe, so there is no owner check.
func dialSocket(ctx context.Context, path string) (net.Conn, error) {
	return platform.DialPipe(ctx, platform.PipeName(path))
}
