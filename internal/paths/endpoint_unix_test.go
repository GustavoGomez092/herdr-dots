//go:build unix

package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"herdr-dots/internal/platform"
)

// shortTemp is a private temp dir with a short path, so a socket under it
// stays within the unix socket path limit (t.TempDir is too deep on macOS).
func shortTemp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// fallbackEnv points the socket fallback at fresh dirs: TMPDIR at tmp and
// XDG_RUNTIME_DIR at xdg ("" unsets it), and no /run/user guess.
func fallbackEnv(t *testing.T, tmp, xdg string) {
	t.Helper()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	old := defaultRuntimeDir
	defaultRuntimeDir = func() string { return "" }
	t.Cleanup(func() { defaultRuntimeDir = old })
}

// longStatePaths has a state dir deep enough that the socket falls back.
func longStatePaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	return New(filepath.Join(root, "c"), filepath.Join(root, strings.Repeat("d", 100)), "/sock")
}

func uidDir(tmp string) string {
	return filepath.Join(tmp, "herdr-dots-"+strconv.Itoa(os.Getuid()))
}

func TestSockFallbackHappyPathMakesAPrivateDir(t *testing.T) {
	tmp := shortTemp(t)
	fallbackEnv(t, tmp, "")
	p := longStatePaths(t)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	dir := uidDir(tmp)
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf("fallback dir %s: %v %v", dir, st, err)
	}
	if got := p.EngineEndpoint(); filepath.Dir(got) != dir {
		t.Fatalf("endpoint %s not in %s", got, dir)
	}
	// A second client (and the engine it starts) computes the same path, and
	// Ensure accepts the directory the first one made.
	q := New(p.ConfigDir, p.StateDir, p.SocketPath)
	if err := q.Ensure(); err != nil {
		t.Fatal(err)
	}
	if q.EngineEndpoint() != p.EngineEndpoint() {
		t.Fatalf("endpoints differ: %s vs %s", q.EngineEndpoint(), p.EngineEndpoint())
	}
}

func TestSockFallbackRefusesASymlink(t *testing.T) {
	tmp := shortTemp(t)
	fallbackEnv(t, tmp, "")
	target := filepath.Join(tmp, "elsewhere")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := uidDir(tmp)
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
	err := longStatePaths(t).Ensure()
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("Ensure = %v, want a refusal naming %s", err, dir)
	}
	if st, _ := os.Stat(target); st.Mode().Perm() != 0o755 {
		t.Fatalf("the link's target was changed to %v", st.Mode().Perm())
	}
}

func TestSockFallbackRefusesADirAnotherUserOwns(t *testing.T) {
	tmp := shortTemp(t)
	fallbackEnv(t, tmp, "")
	dir := uidDir(tmp)
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	// chown needs root: simulate the owner through the platform check.
	old := checkPrivate
	checkPrivate = func(path string) error {
		if path == dir {
			return platform.ErrNotOwner
		}
		return old(path)
	}
	t.Cleanup(func() { checkPrivate = old })

	err := longStatePaths(t).Ensure()
	if err == nil || !strings.Contains(err.Error(), dir) || !errors.Is(err, platform.ErrNotOwner) {
		t.Fatalf("Ensure = %v, want an ErrNotOwner refusal naming %s", err, dir)
	}
	if st, _ := os.Lstat(dir); st.Mode().Perm() != 0o750 {
		t.Fatalf("another user's dir was chmodded to %v", st.Mode().Perm())
	}
}

func TestSockFallbackRefusesAnExistingDirOthersCanUse(t *testing.T) {
	tmp := shortTemp(t)
	fallbackEnv(t, tmp, "")
	dir := uidDir(tmp)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := longStatePaths(t).Ensure()
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("Ensure = %v, want a refusal naming %s", err, dir)
	}
	if st, _ := os.Lstat(dir); st.Mode().Perm() != 0o755 {
		t.Fatalf("existing dir chmodded to %v", st.Mode().Perm())
	}
}

func TestSockFallbackPrefersAPrivateXDGRuntimeDir(t *testing.T) {
	tmp, xdg := shortTemp(t), shortTemp(t)
	if err := os.Chmod(xdg, 0o700); err != nil {
		t.Fatal(err)
	}
	fallbackEnv(t, tmp, xdg)
	p := longStatePaths(t)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if got := p.EngineEndpoint(); filepath.Dir(got) != xdg || !strings.HasPrefix(filepath.Base(got), "herdr-dots-") {
		t.Fatalf("endpoint %s, want herdr-dots-*.sock in %s", got, xdg)
	}
	if _, err := os.Lstat(uidDir(tmp)); !os.IsNotExist(err) {
		t.Fatalf("temp fallback dir made although XDG_RUNTIME_DIR is usable: %v", err)
	}
}

func TestSockFallbackSkipsAnUnusableXDGRuntimeDir(t *testing.T) {
	loose := shortTemp(t)
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(shortTemp(t), "rt")
	private := shortTemp(t)
	if err := os.Chmod(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	for name, xdg := range map[string]string{"loose": loose, "symlink": link, "relative": "run/user", "missing": filepath.Join(loose, "nope")} {
		t.Run(name, func(t *testing.T) {
			tmp := shortTemp(t)
			fallbackEnv(t, tmp, xdg)
			p := longStatePaths(t)
			if err := p.Ensure(); err != nil {
				t.Fatal(err)
			}
			if got := p.EngineEndpoint(); filepath.Dir(got) != uidDir(tmp) {
				t.Fatalf("endpoint %s, want the temp fallback %s", got, uidDir(tmp))
			}
		})
	}
}
