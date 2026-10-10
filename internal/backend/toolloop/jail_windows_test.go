//go:build windows

package toolloop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamsDevicesAndTrailingDotsAreDenied(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("API_KEY=x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".env::$DATA", ".env:secret", "NUL", "src/com1.txt", ".env.", ".env "} {
		if _, _, err := ResolveInRoot(root, p, DefaultDeny); !errors.Is(err, ErrDenied) {
			t.Errorf("%q: err = %v, want ErrDenied", p, err)
		}
	}
}

// junction makes link a directory junction to target (mklink /J needs no
// privilege).
func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
}

// Review Focus (Important): EvalSymlinks does not follow a junction (Go
// 1.23+), so proj\ext -> outside must be caught by the irregular-element
// check, the reparse-point open and the handle's final path.
func TestAJunctionOutOfTheRootIsRefused(t *testing.T) {
	root, outside := repoDir(t)
	junction(t, filepath.Join(root, "ext"), outside)
	for _, p := range []string{"ext/notes.txt", `ext\notes.txt`, filepath.Join(root, "ext", "notes.txt"), "ext"} {
		if _, _, err := ResolveInRoot(root, p, DefaultDeny); err == nil {
			t.Errorf("%q resolved through the junction", p)
		}
	}
	tools := RepoTools(root, DefaultLimits())
	if out, err := run(t, tools, "read_file", map[string]any{"path": "ext/notes.txt"}); err == nil || strings.Contains(out, "PRIVATE") {
		t.Fatalf("read_file through a junction: %q %v", out, err)
	}
	if out, _ := run(t, tools, "grep", map[string]any{"pattern": "PRIVATE"}); strings.Contains(out, "PRIVATE") {
		t.Fatalf("grep reached through the junction: %q", out)
	}
	if out, _ := run(t, tools, "list_files", map[string]any{}); strings.Contains(out, "notes.txt") {
		t.Fatalf("list_files listed through the junction: %q", out)
	}
	// openVerified on its own: the handle's final path names the target.
	if f, err := openVerified(root, filepath.Join(root, "ext", "notes.txt"), "ext/notes.txt"); err == nil {
		f.Close()
		t.Fatal("openVerified opened a file through a junction")
	}
}

func TestDriveRootsAreUnavailable(t *testing.T) {
	for _, r := range []string{`C:\`, `C:`, `\\?\C:\`} {
		if !Unavailable(r) {
			t.Errorf("Unavailable(%q) = false", r)
		}
	}
}

// CI (windows-latest): "/etc/passwd" has no drive, so filepath.IsAbs is false
// and the jail joined it under the root. A rooted or drive-relative path is
// refused as outside the root, for reads and for writes, even when the same
// path exists inside the root.
func TestRootedAndDriveRelativePathsAreOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "passwd"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	vol := filepath.VolumeName(root) // "C:" or "D:"
	for _, p := range []string{"/etc/passwd", `\etc\passwd`, "/", vol + "etc", vol + `..\x`, vol} {
		if _, _, err := ResolveInRoot(root, p, DefaultDeny); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("ResolveInRoot(%q) err = %v, want ErrOutsideRoot", p, err)
		}
		if _, _, _, err := resolveForWrite(root, p, DefaultDeny); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("resolveForWrite(%q) err = %v, want ErrOutsideRoot", p, err)
		}
	}
	if _, rel, err := ResolveInRoot(root, `etc\passwd`, DefaultDeny); err != nil || rel != "etc/passwd" {
		t.Fatalf("the relative spelling must still resolve: %q %v", rel, err)
	}
}
