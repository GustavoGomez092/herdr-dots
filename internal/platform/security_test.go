package platform

import "testing"

func TestPipeName(t *testing.T) {
	sock := `C:\Users\José Núñez\AppData\Roaming\herdr\herdr.sock`
	if got := PipeName(sock); got != `\\.\pipe\`+sock {
		t.Fatalf("PipeName = %s", got)
	}
	for _, p := range []string{`\\.\pipe\herdr`, `\\.\PIPE\x`, `//./pipe/y`} {
		if PipeName(p) != p {
			t.Errorf("pipe path %s changed to %s", p, PipeName(p))
		}
	}
}

func TestWindowsReservedName(t *testing.T) {
	for rel, want := range map[string]bool{
		".env::$DATA":           true,
		"notes.txt:secret":      true,
		"C:/Windows/win.ini":    true,
		"src/NUL":               true,
		"src/nul.txt":           true,
		`logs\com1.log`:         true,
		"CONOUT$":               true,
		"env.":                  true,
		"secrets /prod.json":    true,
		"src/main.go":           false,
		"console/log.go":        false,
		"docs/null.md":          false,
		"./a/../b.txt":          false,
		"my folder/José's.txt":  false,
		".github/workflows/x.y": false,
	} {
		if windowsReservedName(rel) != want {
			t.Errorf("windowsReservedName(%q) = %v", rel, !want)
		}
	}
}

const (
	sidMe     = "S-1-5-21-1111-2222-3333-1001"
	sidOther  = "S-1-5-21-1111-2222-3333-1002"
	sidEvery  = "S-1-1-0"
	sidAuthed = "S-1-5-11"
)

// The owner rules on plain SID strings, so every OS runs them. adminInToken
// is true only for an elevated token: a filtered (non-elevated) token lists
// Administrators as deny-only, which CheckTokenMembership reports as false.
func TestOwnerAccepted(t *testing.T) {
	cases := []struct {
		name         string
		owner        string
		adminInToken bool
		want         bool
	}{
		{"current user", sidMe, false, true},
		{"another user", sidOther, true, false},
		{"Administrators, elevated token", sidAdministrators, true, true},
		{"Administrators, filtered token", sidAdministrators, false, false},
		{"SYSTEM is not accepted as owner", sidSystem, true, false},
		{"Everyone", sidEvery, true, false},
		{"empty", "", true, false},
	}
	for _, c := range cases {
		if got := ownerAccepted(c.owner, sidMe, c.adminInToken); got != c.want {
			t.Errorf("%s: ownerAccepted = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAllowedACESID(t *testing.T) {
	cases := []struct {
		name, sid, owner string
		want             bool
	}{
		{"current user", sidMe, sidMe, true},
		{"the owner", sidOther, sidOther, true},
		{"SYSTEM", sidSystem, sidMe, true},
		{"Administrators", sidAdministrators, sidMe, true},
		{"owner rights", sidOwnerRights, sidMe, true},
		{"Everyone", sidEvery, sidMe, false},
		{"Authenticated Users", sidAuthed, sidMe, false},
		{"another user", sidOther, sidMe, false},
		{"empty", "", sidMe, false},
	}
	for _, c := range cases {
		if got := allowedACESID(c.sid, sidMe, c.owner); got != c.want {
			t.Errorf("%s: allowedACESID = %v, want %v", c.name, got, c.want)
		}
	}
}
