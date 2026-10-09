//go:build windows

package platform

import (
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
	me, err := currentUserSID()
	if err != nil {
		return err
	}
	dacl, err := privateDACL(dir)
	if err != nil {
		return err
	}
	// Owner and DACL in one call: the private DACL grants WRITE_OWNER, and an
	// object left owned by someone else (an Administrators-owned file made
	// while elevated) would stay refused by CheckPrivate.
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		me, nil, dacl, nil); err != nil {
		return fmt.Errorf("make %s private: %w", path, err)
	}
	return CheckPrivate(path)
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
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	return checkPrivateSD(sd, st.IsDir())
}

// CheckPrivateFile is CheckPrivate for a file already open: it reads the
// owner and DACL through the handle, so the file checked is the file read.
func CheckPrivateFile(f *os.File) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return checkPrivateSD(sd, st.IsDir())
}

// checkPrivateSD reads a security descriptor into plain data and applies
// checkPrivateEntries to it.
func checkPrivateSD(sd *windows.SECURITY_DESCRIPTOR, isDir bool) error {
	me, err := currentUserSID()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil {
		return ErrNotOwner
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	var entries []aclEntry
	if dacl != nil {
		for i := uint32(0); i < uint32(dacl.AceCount); i++ {
			var ace *windows.ACCESS_ALLOWED_ACE
			if err := windows.GetAce(dacl, i, &ace); err != nil {
				return err
			}
			e := aclEntry{
				InheritOnly: ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0,
				Inheritable: ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) != 0,
				Kind:        aceOther,
			}
			switch ace.Header.AceType {
			case windows.ACCESS_ALLOWED_ACE_TYPE:
				e.Kind = aceAllow
				e.SID = (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
			case windows.ACCESS_DENIED_ACE_TYPE:
				e.Kind = aceDeny
			}
			entries = append(entries, e)
		}
	}
	return checkPrivateEntries(owner.String(), me.String(), adminInToken(), isDir, dacl != nil, entries)
}

// FixPrivateHint tells the user how to make path private again. It avoids
// %USERNAME%, which PowerShell does not expand.
func FixPrivateHint(path string) string {
	return fmt.Sprintf(`delete the file and set it again, or run: icacls "%s" /inheritance:r /remove:g *S-1-1-0 *S-1-5-11 *S-1-5-32-545`, path)
}

// OpenNoFollow opens path read-only (a file or a directory) without
// following a reparse point: CreateFile with FILE_FLAG_OPEN_REPARSE_POINT
// opens the link itself, and the handle is refused with ErrReparsePoint when
// it is a symlink, junction, mount point or other reparse point (a
// deduplicated file excepted). The check reads the handle that is returned,
// so nothing can be swapped in between. A reparse point higher up the path is
// VerifyOpenedPath's job.
func OpenNoFollow(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var info struct{ FileAttributes, ReparseTag uint32 } // FILE_ATTRIBUTE_TAG_INFO
	if err := windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	if reparseRefused(info.FileAttributes, info.ReparseTag) {
		windows.CloseHandle(h)
		return nil, &os.PathError{Op: "open", Path: path, Err: ErrReparsePoint}
	}
	return os.NewFile(uintptr(h), path), nil
}

// finalPath is the path Windows resolved for handle h (every symlink,
// junction and mount point followed, short names expanded), \\?\-prefixed.
func finalPath(h windows.Handle) (string, error) {
	const flags = 0x0 // VOLUME_NAME_DOS | FILE_NAME_NORMALIZED
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), flags)
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			return windows.UTF16ToString(buf[:n]), nil
		}
		buf = make([]uint16, n+1) // n is the size needed
	}
}

// dirFinalPath is dir's final path. dir itself may be reached through a
// junction (a relocated profile or project); that one is followed.
func dirFinalPath(dir string) (string, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	return finalPath(h)
}

// VerifyOpenedPath checks that f, opened as dir + rel, really is that file:
// the handle's final path must equal dir's final path joined with rel
// (compared without case). A junction or mount point anywhere in rel, which
// filepath.EvalSymlinks does not follow (Go 1.23+), makes them differ, and
// the file is refused with ErrReparsePoint.
func VerifyOpenedPath(f *os.File, dir, rel string) error {
	got, err := finalPath(windows.Handle(f.Fd()))
	if err != nil {
		return fmt.Errorf("read the opened file's path: %w", err)
	}
	want, err := dirFinalPath(dir)
	if err != nil {
		return fmt.Errorf("read the directory's path: %w", err)
	}
	if !finalPathIs(got, want, rel) {
		return ErrReparsePoint
	}
	return nil
}

// IsVolumeRoot reports whether p is a drive or share root: C:\, C:, \\?\C:\
// or \\server\share.
func IsVolumeRoot(p string) bool { return volumeRoot(p) }

// ReservedPathName reports whether a project-relative path (slash or
// backslash separated) names an NTFS alternate data stream ("a.txt:secret",
// ".env::$DATA"), a device (CON, NUL, COM1, ...), or a name Windows silently
// rewrites (a trailing dot or space).
func ReservedPathName(rel string) bool { return windowsReservedName(rel) }
