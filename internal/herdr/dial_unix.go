//go:build unix

package herdr

import (
	"context"
	"net"
)

// dialSocket connects to Herdr's unix socket.
func dialSocket(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}
