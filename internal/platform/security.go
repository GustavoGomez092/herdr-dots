package platform

import "errors"

var (
	// ErrNotPrivate means a file or directory is usable by other users.
	ErrNotPrivate = errors.New("other users can read it")
	// ErrNotOwner means a file, directory or pipe belongs to another user.
	ErrNotOwner = errors.New("it is owned by another user")
)

// Well-known Windows SIDs (string form) that may hold access to a private
// object besides its owner. The comparison rules below take plain strings so
// every OS can unit-test them.
const (
	sidSystem         = "S-1-5-18"
	sidAdministrators = "S-1-5-32-544"
	sidOwnerRights    = "S-1-3-4"
)

// ownerAccepted decides whether an object owned by owner is trusted by the
// user me. The current user always is. Administrators is accepted only when
// adminInToken is true, i.e. this process's token has Administrators enabled
// (an elevated token). Under UAC an elevated process creates files with
// Administrators as the default owner, so refusing that owner would break the
// elevated user's own files and pipes. A non-elevated token lists
// Administrators as deny-only, so adminInToken is false and an
// Administrators-owned object is refused: a different, more privileged
// principal made it.
func ownerAccepted(owner, me string, adminInToken bool) bool {
	if owner == "" {
		return false
	}
	if owner == me {
		return true
	}
	return owner == sidAdministrators && adminInToken
}

// allowedACESID reports whether an allow entry for sid may appear in a
// private object's DACL: the current user, the object's owner, SYSTEM,
// Administrators and OWNER RIGHTS. Anything else (Everyone, Authenticated
// Users, Users, another account) makes it not private.
func allowedACESID(sid, me, owner string) bool {
	if sid == "" {
		return false
	}
	switch sid {
	case me, owner, sidSystem, sidAdministrators, sidOwnerRights:
		return true
	}
	return false
}

// pipeOwnerAccepted: an engine pipe is always created with O:<user SID>, so
// only the current user may own it. Administrators is not accepted here even
// for an elevated client (unlike files, see ownerAccepted).
func pipeOwnerAccepted(owner, me string) bool { return owner != "" && owner == me }

type aceKind int

const (
	aceAllow aceKind = iota // ACCESS_ALLOWED_ACE
	aceDeny                 // ACCESS_DENIED_ACE
	aceOther                // object, callback and every other entry type
)

// aclEntry is one DACL entry reduced to what the privacy rules look at.
type aclEntry struct {
	Kind        aceKind
	InheritOnly bool // does not apply to the object itself
	Inheritable bool // OBJECT_INHERIT or CONTAINER_INHERIT: children inherit it
	SID         string
}

// checkPrivateEntries is CheckPrivate's rule set over plain data, so every OS
// can test it. hasDACL is false for a NULL or absent DACL (full access for
// everyone). On a directory, an allow entry for a foreign SID is refused when
// it applies to the directory or is inheritable (even inherit-only), so new
// files never briefly inherit it. On a file, inherit-only entries apply to
// nothing and are skipped. Deny entries are skipped.
func checkPrivateEntries(owner, me string, adminInToken, isDir, hasDACL bool, entries []aclEntry) error {
	if !ownerAccepted(owner, me, adminInToken) {
		return ErrNotOwner
	}
	if !hasDACL {
		return ErrNotPrivate
	}
	for _, e := range entries {
		applies := !e.InheritOnly || (isDir && e.Inheritable)
		if !applies {
			continue
		}
		switch e.Kind {
		case aceDeny:
		case aceAllow:
			if !allowedACESID(e.SID, me, owner) {
				return ErrNotPrivate
			}
		default:
			return ErrNotPrivate // be conservative
		}
	}
	return nil
}
