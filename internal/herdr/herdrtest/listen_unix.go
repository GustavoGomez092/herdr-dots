//go:build unix

package herdrtest

import "net"

// Listen serves a Herdr-style socket at path the way Herdr does on this OS:
// a unix socket.
func Listen(path string) (net.Listener, error) { return net.Listen("unix", path) }
