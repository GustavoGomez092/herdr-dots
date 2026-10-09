//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// currentUserSID is the SID of the user running this process.
func currentUserSID() (*windows.SID, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read the current user's SID: %w", err)
	}
	return tu.User.Sid, nil
}

// adminInToken reports whether Administrators is enabled in this process's
// token: true for an elevated token, false for a filtered one (where the
// group is deny-only) and for non-members.
func adminInToken() bool {
	admins, err := windows.StringToSid(sidAdministrators)
	if err != nil {
		return false
	}
	member, err := windows.Token(0).IsMember(admins)
	return err == nil && member
}

// ownerOK applies ownerAccepted to SIDs from the Windows API.
func ownerOK(owner, me *windows.SID) bool {
	if owner == nil || me == nil {
		return false
	}
	return ownerAccepted(owner.String(), me.String(), adminInToken())
}

// privateDACL builds "owner-only" access for path: full control for the
// current user, SYSTEM and Administrators, inheritance from the parent off.
// Directories pass their entries on to new children (OICI).
func privateDACL(dir bool) (*windows.ACL, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	inherit := ""
	if dir {
		inherit = "OICI"
	}
	sddl := fmt.Sprintf("D:P(A;%[1]s;FA;;;%[2]s)(A;%[1]s;FA;;;SY)(A;%[1]s;FA;;;BA)", inherit, sid.String())
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	return dacl, err
}

func applyPrivate(path string, dir bool) error {
	if CheckPrivate(path) == nil {
		return nil // already private: don't rewrite (and re-propagate) the ACL
	}
	dacl, err := privateDACL(dir)
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("make %s private: %w", path, err)
	}
	return nil
}

// SecureDir gives a directory an explicit owner-only DACL (current user,
// SYSTEM, Administrators; inheritance off) unless it is already private.
// New files created inside inherit it.
func SecureDir(path string) error { return applyPrivate(path, true) }

// SecureFile gives a file an explicit owner-only DACL unless it is already
// private.
func SecureFile(path string) error { return applyPrivate(path, false) }

// CheckPrivate fails with ErrNotOwner when path's owner is neither the
// current user nor (for an elevated user) Administrators, and with
// ErrNotPrivate when its DACL is missing or allows anyone but the owner, the
// current user, SYSTEM and Administrators. Deny entries and inherit-only
// entries are ignored.
func CheckPrivate(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return checkPrivateSD(sd)
}

// CheckPrivateFile is CheckPrivate for a file already open: it reads the
// owner and DACL through the handle, so the file checked is the file read.
func CheckPrivateFile(f *os.File) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return checkPrivateSD(sd)
}

// checkPrivateSD applies CheckPrivate's rules to a security descriptor.
func checkPrivateSD(sd *windows.SECURITY_DESCRIPTOR) error {
	me, err := currentUserSID()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !ownerOK(owner, me) {
		return ErrNotOwner
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return ErrNotPrivate // no DACL at all grants everyone everything
	}
	meStr, ownerStr := me.String(), owner.String()
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !allowedACESID(sid.String(), meStr, ownerStr) {
				return ErrNotPrivate
			}
		default:
			return ErrNotPrivate // object/callback entries: be conservative
		}
	}
	return nil
}

// FixPrivateHint tells the user how to make path private again.
func FixPrivateHint(path string) string {
	return fmt.Sprintf(`run: icacls "%s" /inheritance:r /grant:r "%%USERNAME%%:F" (or delete it and set it again)`, path)
}

// OpenNoFollow opens path read-only and fails if it is a symlink or another
// reparse point (a junction or mount point).
func OpenNoFollow(path string) (*os.File, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, &os.PathError{Op: "open", Path: path, Err: errors.New("is a symlink or reparse point")}
	}
	return os.Open(path)
}

// ReservedPathName reports whether a project-relative path (slash or
// backslash separated) names an NTFS alternate data stream ("a.txt:secret",
// ".env::$DATA"), a device (CON, NUL, COM1, ...), or a name Windows silently
// rewrites (a trailing dot or space).
func ReservedPathName(rel string) bool { return windowsReservedName(rel) }
