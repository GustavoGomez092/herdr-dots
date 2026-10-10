//go:build unix

package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"herdr-dots/internal/platform"
)

// EngineEndpoint is where the engine serves IPC: the unix socket EngineSock.
func (p Paths) EngineEndpoint() string { return p.EngineSock() }

func (p Paths) ensureEndpoint() error { return p.ensureSockFallback() }

// maxSockPath stays under the 104-byte sun_path limit on macOS.
const maxSockPath = 100

// checkPrivate is platform.CheckPrivate (Lstat: owned by this uid, no group
// or other bits). Tests swap it to simulate another user's directory, since
// chown needs root.
var checkPrivate = platform.CheckPrivate

// defaultRuntimeDir is the per-user runtime dir, /run/user/<uid>.
var defaultRuntimeDir = func() string { return fmt.Sprintf("/run/user/%d", os.Getuid()) }

// runtimeDir is /run/user/<uid> when it is an absolute, real directory that
// only this user owns, with mode 0700; "" otherwise. XDG_RUNTIME_DIR counts
// only when it is that same dir (R8): every client must compute the same
// socket path, and an MCP client started with a minimal environment (Codex
// passes no XDG_RUNTIME_DIR) or from another one (WSLg sets its own) would
// otherwise look somewhere else than the engine. So an XDG_RUNTIME_DIR set
// elsewhere, usable or not, gives the path an unset one gives.
func runtimeDir() string {
	d := defaultRuntimeDir()
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" && filepath.Clean(x) == filepath.Clean(d) {
		d = x
	}
	if d == "" || !filepath.IsAbs(d) {
		return ""
	}
	if st, err := os.Lstat(d); err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 || checkPrivate(d) != nil {
		return ""
	}
	return d
}

// sockFallbackDir returns a per-uid directory in the temp dir for socket
// fallback. ensureSockFallback refuses it unless this user made it private.
func (p Paths) sockFallbackDir() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("herdr-dots-%d", os.Getuid()))
}

// EngineSock is the engine's unix socket. Deep state dirs would exceed the
// unix socket path limit, so those fall back to a short path: the per-user
// runtime dir when it is usable, else a private directory in the temp dir.
// Every client computes it the same way from the same environment.
// Use EngineEndpoint, which is this on Unix and a named pipe on Windows.
func (p Paths) EngineSock() string {
	s := filepath.Join(p.SessionDir, "dots.sock")
	if len(s) <= maxSockPath {
		return s
	}
	key := SessionKey(p.SocketPath)
	if d := runtimeDir(); d != "" {
		if s := filepath.Join(d, "herdr-dots-"+key+".sock"); len(s) <= maxSockPath {
			return s
		}
	}
	return filepath.Join(p.sockFallbackDir(), key+".sock")
}

// ensureSockFallback creates the per-user temp directory when the socket
// falls back to it (Unix only; see EngineSock). It is made with Mkdir and
// never chmodded: it is refused, by name, unless it is a real directory (not
// a symlink) that this user owns with mode 0700.
func (p Paths) ensureSockFallback() error {
	sock := p.EngineSock()
	dir := p.sockFallbackDir()
	if filepath.Dir(sock) != dir {
		return nil // the session dir, or a runtime dir runtimeDir checked
	}
	if len(sock) > maxSockPath {
		return fmt.Errorf("engine socket path too long: %s", sock)
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("engine socket directory %s: %w", dir, err)
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("engine socket directory %s: %w", dir, err)
	}
	const fix = "refusing to use it: remove it, and the next start makes it again, private"
	if !st.IsDir() {
		return fmt.Errorf("engine socket directory %s is not a directory (a symlink or a file); %s", dir, fix)
	}
	if err := checkPrivate(dir); err != nil {
		return fmt.Errorf("engine socket directory %s is unsafe (%w); %s", dir, err, fix)
	}
	if st.Mode().Perm() != 0o700 {
		return fmt.Errorf("engine socket directory %s has mode %v, want 0700; %s", dir, st.Mode().Perm(), fix)
	}
	return nil
}
