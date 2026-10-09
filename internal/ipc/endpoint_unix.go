//go:build unix

package ipc

import (
	"context"
	"net"
	"os"
)

// Listen serves the engine endpoint, a unix socket path. A stale socket from
// a dead engine is removed first (the caller holds the engine lock), and the
// socket is made 0600. Closing the listener removes the socket file.
func Listen(endpoint string) (net.Listener, error) {
	_ = os.Remove(endpoint)
	ln, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(endpoint, 0o600)
	return ln, nil
}

// Dial connects to the engine endpoint.
func Dial(ctx context.Context, endpoint string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", endpoint)
}
