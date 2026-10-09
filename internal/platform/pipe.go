package platform

import "strings"

// PipePrefix starts every local named pipe path on Windows.
const PipePrefix = `\\.\pipe\`

// PipeName maps a Herdr socket path to the named pipe Herdr serves on
// Windows. Herdr writes a marker file at HERDR_SOCKET_PATH and listens on
// \\.\pipe\<that path> (its interprocess crate's namespaced name). A value
// that already is a pipe path is returned unchanged.
func PipeName(socketPath string) string {
	low := strings.ToLower(socketPath)
	if strings.HasPrefix(low, `\\.\pipe\`) || strings.HasPrefix(low, `//./pipe/`) {
		return socketPath
	}
	return PipePrefix + socketPath
}
