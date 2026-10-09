package platform

import "testing"

func TestWindowsReservedDeviceNamesIncludeZeroAndSuperscripts(t *testing.T) {
	for rel, want := range map[string]bool{
		"COM0":          true,
		"src/lpt0.txt":  true,
		"COM¹":          true,
		"COM².log":      true,
		"a/LPT³":        true,
		"lpt¹.txt":      true,
		"COM10":         false,
		"com¹x":         false,
		"src/COM_ports": false,
	} {
		if windowsReservedName(rel) != want {
			t.Errorf("windowsReservedName(%q) = %v", rel, !want)
		}
	}
}

// Engine pipes are always created with O:<user SID>, so a pipe owner other
// than the current user (Administrators included) is never legitimate.
func TestPipeOwnerAccepted(t *testing.T) {
	for name, c := range map[string]struct {
		owner string
		want  bool
	}{
		"current user":                 {sidMe, true},
		"Administrators, never a pipe": {sidAdministrators, false},
		"SYSTEM":                       {sidSystem, false},
		"another user":                 {sidOther, false},
		"empty":                        {"", false},
	} {
		if got := pipeOwnerAccepted(c.owner, sidMe); got != c.want {
			t.Errorf("%s: pipeOwnerAccepted = %v, want %v", name, got, c.want)
		}
	}
}

func TestCheckPrivateEntries(t *testing.T) {
	allow := func(sid string) aclEntry { return aclEntry{Kind: aceAllow, SID: sid} }
	deny := func(sid string) aclEntry { return aclEntry{Kind: aceDeny, SID: sid} }
	other := func(sid string) aclEntry { return aclEntry{Kind: aceOther, SID: sid} }
	inh := func(e aclEntry) aclEntry { e.Inheritable = true; return e }
	inhOnly := func(e aclEntry) aclEntry { e.InheritOnly, e.Inheritable = true, true; return e }
	cases := []struct {
		name         string
		owner        string
		adminInToken bool
		isDir        bool
		hasDACL      bool
		entries      []aclEntry
		want         error
	}{
		{"NULL DACL grants everyone everything", sidMe, false, false, false, nil, ErrNotPrivate},
		{"absent DACL is refused too", sidMe, false, true, false, []aclEntry{}, ErrNotPrivate},
		{"empty DACL grants nobody", sidMe, false, false, true, nil, nil},
		{"user, SYSTEM, Administrators", sidMe, false, false, true,
			[]aclEntry{allow(sidMe), allow(sidSystem), allow(sidAdministrators)}, nil},
		{"Everyone allowed", sidMe, false, false, true, []aclEntry{allow(sidMe), allow(sidEvery)}, ErrNotPrivate},
		{"Authenticated Users allowed", sidMe, false, false, true, []aclEntry{allow(sidAuthed)}, ErrNotPrivate},
		{"deny entry for Everyone is skipped", sidMe, false, false, true,
			[]aclEntry{deny(sidEvery), allow(sidMe)}, nil},
		{"inherit-only Everyone on a file is skipped", sidMe, false, false, true,
			[]aclEntry{allow(sidMe), inhOnly(allow(sidEvery))}, nil},
		{"inherit-only Everyone on a dir is refused", sidMe, false, true, true,
			[]aclEntry{allow(sidMe), inhOnly(allow(sidEvery))}, ErrNotPrivate},
		{"inheritable Everyone on a dir is refused", sidMe, false, true, true,
			[]aclEntry{inh(allow(sidEvery))}, ErrNotPrivate},
		{"inherit-only without inherit flags applies to nothing", sidMe, false, true, true,
			[]aclEntry{{Kind: aceAllow, SID: sidEvery, InheritOnly: true}}, nil},
		{"inheritable owner entries on a dir", sidMe, false, true, true,
			[]aclEntry{inh(allow(sidMe)), inhOnly(allow(sidSystem))}, nil},
		{"object/callback entry is refused", sidMe, false, false, true, []aclEntry{other(sidMe)}, ErrNotPrivate},
		{"foreign owner", sidOther, true, false, true, []aclEntry{allow(sidMe)}, ErrNotOwner},
		{"foreign owner beats a bad DACL", sidOther, false, false, false, nil, ErrNotOwner},
		{"Administrators owner, filtered token", sidAdministrators, false, false, true,
			[]aclEntry{allow(sidMe)}, ErrNotOwner},
		{"Administrators owner, elevated token", sidAdministrators, true, false, true,
			[]aclEntry{allow(sidMe), allow(sidAdministrators)}, nil},
	}
	for _, c := range cases {
		got := checkPrivateEntries(c.owner, sidMe, c.adminInToken, c.isDir, c.hasDACL, c.entries)
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
