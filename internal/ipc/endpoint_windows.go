//go:build windows

package ipc

import (
	"context"
	"net"

	"herdr-dots/internal/platform"
)

// Listen serves the engine endpoint, a named pipe whose security descriptor
// lets only the current user connect. It fails if the name already exists.
func Listen(endpoint string) (net.Listener, error) { return platform.ListenPipe(endpoint) }

// Dial connects to the engine endpoint and refuses a pipe that anyone but the
// current user owns (one created under the engine's name before the engine
// started). The check runs before anything is written to the connection.
func Dial(ctx context.Context, endpoint string) (net.Conn, error) {
	conn, err := platform.DialPipe(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if err := platform.CheckPipeOwner(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
