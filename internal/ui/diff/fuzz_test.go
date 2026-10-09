package diff

import (
	"strings"
	"testing"
	"unicode"
)

// FuzzDiffViewParse feeds arbitrary patches to the viewer's parser: the
// patch text comes from a model. One file per "diff --git " line, hunk
// indexes inside the file, and no line it keeps for the screen carries a
// control, tab, newline or format character.
func FuzzDiffViewParse(f *testing.F) {
	f.Add([]byte("diff --git a/x.go b/x.go\nindex 1111111..2222222 100755\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n"))
	f.Add([]byte("diff --git a/l b/l\nnew file mode 120000\n@@ -0,0 +1 @@\n+\x1b]52;c;aGk=\x07target\n"))
	f.Add([]byte("diff --git a/b.bin b/b.bin\nGIT binary patch\nliteral 3\nKcmV+\n"))
	f.Add([]byte("noise\ndiff --git a/‮q b/‮q\n"))
	f.Fuzz(func(t *testing.T, patch []byte) {
		files := Parse(patch)
		want := 0
		for _, l := range strings.Split(strings.TrimRight(string(patch), "\n"), "\n") {
			if strings.HasPrefix(l, "diff --git ") {
				want++
			}
		}
		if len(files) != want {
			t.Fatalf("%d files for %d \"diff --git\" lines", len(files), want)
		}
		unsafe := func(r rune) bool {
			return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
		}
		for _, fl := range files {
			if strings.IndexFunc(fl.Path, unsafe) >= 0 {
				t.Fatalf("path %q reaches the screen raw", fl.Path)
			}
			for _, h := range fl.Hunks {
				if h < 0 || h >= len(fl.Lines) {
					t.Fatalf("hunk index %d outside %d lines", h, len(fl.Lines))
				}
			}
			for _, l := range fl.Lines {
				if strings.IndexFunc(l, unsafe) >= 0 {
					t.Fatalf("line %q reaches the screen raw", l)
				}
			}
		}
	})
}
