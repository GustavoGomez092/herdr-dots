//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// setSDDL replaces path's DACL with the one in sddl (a file copied from
// another machine, or left world-readable).
func setSDDL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func daclString(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

func mySID(t *testing.T) string {
	t.Helper()
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	return sid.String()
}

func TestSecureFileReplacesAPermissiveDACL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	os.WriteFile(p, []byte("x"), 0o600)
	setSDDL(t, p, "D:(A;;FA;;;WD)") // Everyone: full control
	if err := CheckPrivate(p); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Everyone ACE: %v, want ErrNotPrivate", err)
	}
	if err := SecureFile(p); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(p); err != nil {
		t.Fatalf("after SecureFile: %v", err)
	}
	// Compared by SID, not in the SDDL text: SDDL writes a well-known
	// account by its alias, and CI's runneradmin is the built-in
	// Administrator account, rendered "LA" instead of its S-1-5-21-...-500.
	protected, allowed := daclAllows(t, p)
	want := []string{mySID(t), sidSystem, sidAdministrators} // privateDACL (ruling P6-R2: files keep Administrators)
	slices.Sort(allowed)
	slices.Sort(want)
	if !protected || !slices.Equal(allowed, want) {
		t.Fatalf("DACL %s (allows %v): want protected, allowing only %v", daclString(t, p), allowed, want)
	}
}

// daclAllows reports whether path's DACL is protected (no inheritance) and
// the SIDs its allow entries grant, in S-1-... form.
func daclAllows(t *testing.T, path string) (protected bool, sids []string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("DACL: %v (nil %v)", err, dacl == nil)
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("entry %d has type %d, want only allow entries", i, ace.Header.AceType)
		}
		sids = append(sids, (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String())
	}
	return control&windows.SE_DACL_PROTECTED != 0, sids
}

func TestCheckPrivateRefusesReadAccessForOthers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	os.WriteFile(p, []byte("x"), 0o600)
	setSDDL(t, p, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FR;;;AU)", mySID(t))) // Authenticated Users may read
	if err := CheckPrivate(p); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Authenticated Users read: %v, want ErrNotPrivate", err)
	}
	setSDDL(t, p, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;SY)(A;;FA;;;BA)", mySID(t)))
	if err := CheckPrivate(p); err != nil {
		t.Fatalf("owner, SYSTEM, Administrators: %v", err)
	}
	if h := FixPrivateHint(p); !strings.Contains(h, "icacls") || !strings.Contains(h, p) {
		t.Fatalf("hint %q", h)
	}
}

func TestCheckPrivateFileReadsTheHandlesDACL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	os.WriteFile(p, []byte("x"), 0o600)
	setSDDL(t, p, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FR;;;WD)", mySID(t))) // Everyone may read
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := CheckPrivateFile(f); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Everyone read: %v, want ErrNotPrivate", err)
	}
	setSDDL(t, p, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;SY)", mySID(t)))
	if err := CheckPrivateFile(f); err != nil {
		t.Fatalf("owner and SYSTEM: %v", err)
	}
}

func TestSecureDirIsInheritedByNewFiles(t *testing.T) {
	d := filepath.Join(t.TempDir(), "memory")
	os.Mkdir(d, 0o700)
	setSDDL(t, d, "D:(A;OICI;FA;;;WD)")
	if err := SecureDir(d); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(d, "entry.json")
	os.WriteFile(f, []byte("{}"), 0o600)
	if err := CheckPrivate(f); err != nil {
		t.Fatalf("new file in a secured dir: %v", err)
	}
}

func TestOpenNoFollowRefusesSymlinksAndReservedNames(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	os.WriteFile(real, []byte("ok"), 0o600)
	f, err := OpenNoFollow(real)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks need Developer Mode or admin here: %v", err)
	}
	if f, err := OpenNoFollow(link); err == nil {
		f.Close()
		t.Fatal("symlink opened")
	}
	if !ReservedPathName(".env::$DATA") || !ReservedPathName("src/NUL") || ReservedPathName("src/main.go") {
		t.Fatal("ReservedPathName rules")
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

// Review Focus (Important): a junction is a reparse point OpenNoFollow
// refuses on the handle it opened, with no Lstat-to-open gap.
func TestOpenNoFollowRefusesAJunction(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(dir, "ext")
	junction(t, link, outside)
	if f, err := OpenNoFollow(link); err == nil {
		f.Close()
		t.Fatal("junction opened")
	} else if !errors.Is(err, ErrReparsePoint) {
		t.Fatalf("err = %v, want ErrReparsePoint", err)
	}
	if _, err := OpenNoFollow(filepath.Join(dir, "missing.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v, want ErrNotExist", err)
	}
	if f, err := OpenNoFollow(dir); err != nil {
		t.Fatalf("a plain directory: %v", err)
	} else {
		f.Close()
	}
}

// The handle's final path shows a junction anywhere above the file.
func TestVerifyOpenedPathCatchesAJunctionParent(t *testing.T) {
	proj, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, "notes.txt"), []byte("outside"), 0o600)
	os.WriteFile(filepath.Join(proj, "real.txt"), []byte("inside"), 0o600)
	junction(t, filepath.Join(proj, "ext"), outside)
	f, err := OpenNoFollow(filepath.Join(proj, "ext", "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := VerifyOpenedPath(f, proj, "ext/notes.txt"); err == nil {
		t.Fatal("a file reached through a junction passed")
	}
	g, err := OpenNoFollow(filepath.Join(proj, "real.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if err := VerifyOpenedPath(g, proj, "real.txt"); err != nil {
		t.Fatalf("a plain file: %v", err)
	}
	// A root that is itself reached through a junction is fine.
	alias := filepath.Join(t.TempDir(), "alias")
	junction(t, alias, proj)
	h, err := OpenNoFollow(filepath.Join(alias, "real.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := VerifyOpenedPath(h, alias, "real.txt"); err != nil {
		t.Fatalf("root through a junction: %v", err)
	}
}

func TestIsVolumeRoot(t *testing.T) {
	for _, p := range []string{`C:\`, `C:`, `\\?\C:\`} {
		if !IsVolumeRoot(p) {
			t.Errorf("IsVolumeRoot(%q) = false", p)
		}
	}
	if IsVolumeRoot(t.TempDir()) {
		t.Error("a temp dir is not a volume root")
	}
}

func TestPipeSDDLGrantsOnlyTheUser(t *testing.T) {
	s, err := PipeSDDL()
	if err != nil {
		t.Fatal(err)
	}
	if s != fmt.Sprintf("O:%[1]sD:P(A;;GA;;;%[1]s)", mySID(t)) {
		t.Fatalf("SDDL %s", s)
	}
}

func TestPipeRoundTripOwnerCheckAndNoSecondListener(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\herdr-dots-test-%d`, time.Now().UnixNano())
	ln, err := ListenPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	if _, err := ListenPipe(name); err == nil {
		t.Fatal("a second listener on the same name must fail")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := DialPipe(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := CheckPipeOwner(conn); err != nil {
		t.Fatalf("own pipe refused: %v", err)
	}
	conn.Write([]byte("ping\n"))
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping\n" {
		t.Fatalf("echo %q %v", buf, err)
	}
}

// An absolute path is never RootRelative; a rooted or drive-relative one is.
func TestRootRelativeOnWindows(t *testing.T) {
	for p, want := range map[string]bool{`C:\x`: false, `c:/x`: false, `\\server\share\x`: false, `\\?\C:\x`: false,
		`/etc/passwd`: true, `\etc`: true, `C:x`: true, `main.go`: false} {
		if got := RootRelative(p); got != want {
			t.Errorf("RootRelative(%q) = %v, want %v", p, got, want)
		}
	}
}
