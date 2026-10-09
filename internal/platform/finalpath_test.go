package platform

import "testing"

// GetFinalPathNameByHandle returns \\?\-prefixed paths; the comparison
// strips that, ignores case and trailing separators, and runs on every OS.
func TestNormalizeFinalPath(t *testing.T) {
	for in, want := range map[string]string{
		`\\?\C:\Users\me\proj`:    `C:\Users\me\proj`,
		`\\?\UNC\srv\share\x`:     `\\srv\share\x`,
		`\\?\C:\`:                 `C:\`,
		`C:\`:                     `C:\`,
		`C:`:                      `C:\`,
		`C:\a\`:                   `C:\a`,
		`C:/a/b`:                  `C:\a\b`,
		`\\?\c:\proj\.\a\..\b.go`: `c:\proj\b.go`,
	} {
		if got := normalizeFinalPath(in); got != want {
			t.Errorf("normalizeFinalPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFinalPathIs(t *testing.T) {
	for _, c := range []struct {
		final, dir, rel string
		ok              bool
	}{
		{`\\?\C:\Proj\src\a.go`, `C:\proj`, "src/a.go", true},
		{`\\?\C:\proj\src\a.go`, `\\?\C:\proj\`, `src\a.go`, true},
		{`\\?\UNC\srv\share\proj\a.go`, `\\srv\share\proj`, "a.go", true},
		{`\\?\C:\a.txt`, `C:\`, "a.txt", true},
		{`\\?\C:\a.txt`, `C:`, "a.txt", true},
		{`\\?\C:\a.txt`, `\\?\C:\`, "a.txt", true},
		// A junction proj\ext -> D:\outside: the handle names the target.
		{`\\?\D:\outside\notes.txt`, `C:\proj`, "ext/notes.txt", false},
		{`\\?\C:\outside\notes.txt`, `C:\proj`, "ext/notes.txt", false},
		// Same tail, different parent.
		{`\\?\C:\proj2\a.go`, `C:\proj`, "a.go", false},
		{`\\?\C:\proj\a.go`, `C:\proj`, "b.go", false},
		{``, `C:\proj`, "a.go", false},
	} {
		if got := finalPathIs(c.final, c.dir, c.rel); got != c.ok {
			t.Errorf("finalPathIs(%q, %q, %q) = %v, want %v", c.final, c.dir, c.rel, got, c.ok)
		}
	}
}

func TestVolumeRoot(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\`: true, `C:`: true, `\\?\C:\`: true, `c:/`: true, `\\srv\share`: true, `\\?\UNC\srv\share\`: true,
		`C:\proj`: false, `\\srv\share\proj`: false, `\\?\C:\proj`: false, `proj`: false, ``: false,
	} {
		if got := volumeRoot(p); got != want {
			t.Errorf("volumeRoot(%q) = %v, want %v", p, got, want)
		}
	}
}

// A handle opened with FILE_FLAG_OPEN_REPARSE_POINT is refused when it is a
// reparse point (symlink, junction, mount point, cloud placeholder), except
// a deduplicated file, which Go and Windows treat as a regular file.
func TestReparseRefused(t *testing.T) {
	const reparse, normal = 0x400, 0x80
	for _, c := range []struct {
		attrs, tag uint32
		refused    bool
	}{
		{normal, 0, false},
		{reparse, 0xA000000C, true},           // symlink
		{reparse, 0xA0000003, true},           // mount point / junction
		{reparse, 0x9000001A, true},           // cloud files placeholder
		{reparse | normal, 0x80000013, false}, // dedup
	} {
		if got := reparseRefused(c.attrs, c.tag); got != c.refused {
			t.Errorf("reparseRefused(%#x, %#x) = %v, want %v", c.attrs, c.tag, got, c.refused)
		}
	}
}
