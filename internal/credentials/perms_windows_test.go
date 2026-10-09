//go:build windows

package credentials

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// setDACL replaces path's DACL with sddl.
func setDACL(t *testing.T, path, sddl string) {
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

func me(t *testing.T) string {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return tu.User.Sid.String()
}

// readableByOthers gives Everyone read access, as a file copied from a
// shared folder or another machine can carry.
func readableByOthers(t *testing.T, path string) {
	t.Helper()
	setDACL(t, path, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FR;;;WD)", me(t)))
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// Review Focus: a credentials file copied with inherited Users access.
func TestInheritedUsersAccessIsRefusedAndSetFixesNothingSilently(t *testing.T) {
	f := credFile(t)
	if err := os.WriteFile(f.Path, []byte("[openai]\napi_key = \"sk-xxxxxxxxxxxxxxxxxxxx\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setDACL(t, f.Path, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FR;;;BU)", me(t))) // BUILTIN\Users may read
	if _, _, err := f.Lookup("openai"); !errors.Is(err, ErrInsecurePerms) {
		t.Fatalf("err = %v, want ErrInsecurePerms", err)
	}
	if err := f.Set("openai", "sk-yyyyyyyyyyyyyyyyyyyy"); !errors.Is(err, ErrInsecurePerms) {
		t.Fatalf("Set must refuse too: %v", err)
	}
}
