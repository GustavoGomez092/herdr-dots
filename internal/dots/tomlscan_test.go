package dots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A repository's dot file is read before anything is trusted, so a TOML
// bomb in it must be refused fast and with a short error.
func TestDecodeProjectRefusesTOMLBombsFast(t *testing.T) {
	n := MaxProjectFileBytes / 4
	bombs := map[string]string{
		"64 KB dotted key":           strings.Repeat("a.", 32<<10-8) + "a = 1",
		"64 KB nested inline tables": "x = " + strings.Repeat("{a=", n-4) + "1" + strings.Repeat("}", n-4),
		"64 KB nested arrays":        "x = " + strings.Repeat("[", n*2-8) + strings.Repeat("]", n*2-8),
	}
	for name, bomb := range bombs {
		if len(bomb) > MaxProjectFileBytes {
			t.Fatalf("%s: %d bytes is over the project file cap", name, len(bomb))
		}
		start := time.Now()
		_, err := DecodeProject(ProjectFile{Base: "bomb", Rel: ".herdr-dots/dots/bomb.toml", Data: []byte(bomb)}, projID)
		if took := time.Since(start); took > 100*time.Millisecond {
			t.Errorf("%s: took %v", name, took)
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if len(err.Error()) >= 1<<10 {
			t.Errorf("%s: error is %d bytes", name, len(err.Error()))
		}
	}
	if _, err := DecodeProject(ProjectFile{Base: "a11y", Rel: "x", Data: []byte(a11yTOML)}, projID); err != nil {
		t.Fatalf("a real project dot: %v", err)
	}
}

func TestDecodeStrictListsAFewCleanUnknownKeys(t *testing.T) {
	var b strings.Builder
	b.WriteString(a11yTOML)
	b.WriteString("\n[zz]\n\"\\u202eevil\\u009b\" = 1\n")
	for i := 0; i < 5000; i++ {
		b.WriteString("k")
		b.WriteString(strings.Repeat("x", 40))
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(strings.Repeat("y", i%7))
		b.WriteString("_")
		b.WriteString(strings.Repeat("z", i/26))
		b.WriteString(" = 1\n")
	}
	_, err := DecodeStrict([]byte(b.String()))
	if err == nil {
		t.Fatal("unknown keys accepted")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\u202e\u009b") {
		t.Fatalf("raw bidi/C1 in %q", msg)
	}
	if len(msg) >= 1<<10 || !strings.Contains(msg, "more") {
		t.Fatalf("not capped (%d bytes): %.200q", len(msg), msg)
	}
}

func TestDecodeStrictCleansAndCapsTheDecodeError(t *testing.T) {
	key := "\u202e" + strings.Repeat("k\u009b", 20000)
	data := "\"" + key + "\" = 1\n\"" + key + "\" = 2\n"
	_, err := DecodeStrict([]byte(data))
	if err == nil {
		t.Fatal("duplicate key accepted")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\u202e\u009b") {
		t.Fatalf("raw bidi/C1 in %.200q", msg)
	}
	if len(msg) >= 1<<10 {
		t.Fatalf("error is %d bytes", len(msg))
	}
}

// A parse error still names its line, so `dots list` can point at it.
func TestLoadProjectKeepsTheParseErrorLine(t *testing.T) {
	root := t.TempDir()
	writeProjectDot(t, root, "broken.toml", "name = \"x\"\npurpose = \"y\"\nthis is not toml\n")
	_, bad := LoadProject(root, projID)
	if len(bad) != 1 || bad[0].Line != 3 {
		t.Fatalf("bad %+v", bad)
	}
}

func TestLoadAllRefusesAUserDotBombFast(t *testing.T) {
	dir := t.TempDir()
	bomb := strings.Repeat("a.", 32<<10) + "a = 1\n"
	if err := os.WriteFile(filepath.Join(dir, "bomb.toml"), []byte(bomb), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ds, bad := LoadAll(dir)
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Errorf("took %v", took)
	}
	if len(ds) != 0 || len(bad) != 1 || len(bad[0].Error()) >= 1<<10 {
		t.Fatalf("ds %d bad %v", len(ds), bad)
	}
}

func TestPreScanTOMLLimits(t *testing.T) {
	for _, s := range []string{
		"x = [[[[{a=[[[1]]]}]]]]",
		"a.b.c.d.e.f.g.h = 1",
		"[a.b.c.d.e.f.g.h]",
		"\"a\" . 'b' . c.d.e.f.g.h = 1",
		"x = 1.5\ny = 2026-10-21T10:00:00Z",
	} {
		if err := PreScanTOML([]byte(s)); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{
		"x = [[[[{a=[[[[1]]]]}]]]]",
		"a.b.c.d.e.f.g.h.i = 1",
		"[a.b.c.d.e.f.g.h.i]",
		"\"a\" . 'b' . c.d.e.f.g.h.\"\"\"i\"\"\" = 1",
		"x = {a={b={c={d={e={f={g={h={i=1}}}}}}}}}",
	} {
		if err := PreScanTOML([]byte(s)); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestPreScanTOMLSkipsStringsAndComments(t *testing.T) {
	lit := "# {{{{{{{{{{ [[[[[[[[[[ a.b.c.d.e.f.g.h.i.j\n[pack]\nformat = 1\nnote = 'C:\\{{{{{{{{{{'\n[dot]\nname = '''multi {{{{{{{{{\n[[[[[[[[[ line'''\npurpose = \"a.b.c.d.e.f.g.h.i.j \\\" {{{{{{{{{{\"\n"
	if err := PreScanTOML([]byte(lit)); err != nil {
		t.Fatalf("strings or comments counted: %v", err)
	}
}

// CountKeysAndTables counts '=' and '[' outside comments and all four
// string kinds (H11).
func TestCountKeysAndTablesSkipsStringsAndComments(t *testing.T) {
	data := "a = 1 # x = [y]\n" +
		`b = "=[\"=[" ` + "\n" +
		"c = '=['\n" +
		`d = """` + "\n=[\n" + `"""` + "\n" +
		"e = '''\n=[\n'''\n" +
		"[t]\n[[arr]]\nf = [1, 2]\n"
	keys, tables := CountKeysAndTables([]byte(data))
	if keys != 6 || tables != 4 {
		t.Fatalf("keys %d tables %d, want 6 and 4", keys, tables)
	}
}

func TestCleanTextBoundsItsOutput(t *testing.T) {
	got := CleanText(strings.Repeat("\u202e", 1000), 80)
	if strings.ContainsRune(got, '\u202e') || len([]rune(got)) > 81 {
		t.Fatalf("%d runes: %q", len([]rune(got)), got)
	}
	if got := CleanText("plain", 80); got != "plain" {
		t.Fatalf("%q", got)
	}
}
