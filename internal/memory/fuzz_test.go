package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func FuzzImportTOML(f *testing.F) {
	if data, err := ExportTOML(tomlFixture()); err == nil {
		f.Add(string(data))
	}
	f.Add("version = 1\n[[entry]]\nref = \"p:m1\"\nkind = \"note\"\ntext = \"x\"\n")
	f.Add("version = 99\n")
	f.Fuzz(func(t *testing.T, data string) {
		es, err := ImportTOML([]byte(data))
		if err != nil {
			return
		}
		for _, e := range es {
			if e.Scope != ScopeProject && e.Scope != ScopeDot && e.Scope != ScopeGlobal {
				t.Fatalf("imported scope %q", e.Scope)
			}
		}
		// An import exports and imports again without loss.
		out, err := ExportTOML(es)
		if err != nil {
			t.Fatalf("re-export: %v", err)
		}
		back, err := ImportTOML(out)
		if err != nil {
			t.Fatalf("re-import of our own export: %v\n%s", err, out)
		}
		if len(back) != len(es) {
			t.Fatalf("round trip: %d entries became %d", len(es), len(back))
		}
		for i := range es {
			a, b := es[i], back[i]
			if a.ID != b.ID || a.Scope != b.Scope || a.Dot != b.Dot || a.Kind != b.Kind || a.Status != b.Status ||
				a.Pinned != b.Pinned || a.Text != b.Text || a.Source != b.Source || a.From != b.From || strings.Join(a.Paths, "\x00") != strings.Join(b.Paths, "\x00") {
				t.Fatalf("round trip changed entry %d: %+v -> %+v", i+1, a, b)
			}
		}
	})
}

// FuzzScopeFileLoad feeds arbitrary bytes to the scope-file loader through the
// store: it must report ErrCorrupt or ErrVersion, or return clean entries.
func FuzzScopeFileLoad(f *testing.F) {
	f.Add(`{"version":1,"next":1,"entries":[{"id":"m1","scope":"project","kind":"note","text":"x","status":"active","source":"user"}]}`)
	f.Add(`{"version":2,"entries":[]}`)
	f.Add(`{"entries":[{"text":"\u001b]52;c;aGk=\u0007hand edit"}]}`)
	f.Add(`{"version":1,"next":-5,"entries":[{"id":"m-3"},{"id":""}]}`)
	f.Fuzz(func(t *testing.T, data string) {
		s := Open(filepath.Join(t.TempDir(), "memory"), DefaultLimits())
		dir := filepath.Join(s.Dir, "g-0123456789abcdef")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "project.json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		es, err := s.List("g-0123456789abcdef", Filter{Scope: ScopeProject})
		if err != nil {
			if !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrVersion) {
				t.Fatalf("unexpected error kind: %v", err)
			}
			return
		}
		for _, e := range es {
			if strings.IndexFunc(e.Text, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }) >= 0 {
				t.Fatalf("hand-edited text not sanitized: %q", e.Text)
			}
			if idNum(e.ID) == 0 {
				t.Fatalf("entry without a usable id: %+v", e)
			}
		}
	})
}

func FuzzScanSecrets(f *testing.F) {
	for _, s := range []string{
		"AKIAABCDEFGHIJKLMNOP", "password = hunter2hunter2", "Bearer abcdefghijklmnop1234", "plain prose",
		"sk-ant-" + strings.Repeat("a1B2", 8), "https://user:pass@example.com/x",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		found, kind := ScanSecrets(text)
		if found == (kind == "") {
			t.Fatalf("ScanSecrets(%q) = %v, %q", text, found, kind)
		}
	})
}
