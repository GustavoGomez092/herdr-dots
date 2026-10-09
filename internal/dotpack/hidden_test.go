package dotpack

import (
	"errors"
	"herdr-dots/internal/dots"
	"strings"
	"testing"
)

func TestParseRefusesInvisibleCarriersInMetaAndMemory(t *testing.T) {
	base := dpPackWith(t, "")
	for _, c := range []string{`︀`, `\U000E0100`, `͏`, `ᅟ`, `ᅠ`, `ㅤ`, `ﾠ`, `⠀`, `᠋`, `᠏`, `️`} {
		for name, data := range map[string]string{
			"memory": base + memBlock("dot", "a "+c+" b", ""),
			"path":   base + memBlock("dot", "ok", `"a`+c+`b"`),
			"note":   strings.Replace(base, `note = "n"`, `note = "a`+c+`b"`, 1),
		} {
			if _, err := Parse([]byte(data)); !errors.Is(err, ErrFormat) {
				t.Errorf("%s %s accepted: %v", name, c, err)
			}
		}
	}
}

func TestParseDecodeErrorEscapesHiddenCharacters(t *testing.T) {
	data := "[pack]\nformat = 1\nbad ‮ = = ​\n"
	_, err := Parse([]byte(data))
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("err %v", err)
	}
	for _, r := range err.Error() {
		if r == 0x202e || r == 0x200b {
			t.Fatalf("error echoes a hidden character: %q", err)
		}
	}
	if len([]rune(err.Error())) > 400 {
		t.Fatalf("error not capped: %d", len([]rune(err.Error())))
	}
}

func TestParseCapsMemoryErrors(t *testing.T) {
	var tail strings.Builder
	for i := 0; i < 3000; i++ {
		tail.WriteString(memBlock("nowhere", "x", ""))
	}
	_, err := Parse([]byte(dpPackWith(t, tail.String())))
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("err %v", err)
	}
	if n := len(err.Error()); n > 2000 {
		t.Fatalf("error is %d bytes", n)
	}
	if !strings.Contains(err.Error(), "more") {
		t.Fatalf("no 'and N more': %v", err)
	}
}

func TestParseNormalizesCRLFInMemoryText(t *testing.T) {
	p, err := Parse([]byte(dpPackWith(t, memBlock("dot", `line one\r\nline two`, ""))))
	if err != nil || p.Memory[0].Text != "line one\nline two" {
		t.Fatalf("%v %+v", err, p.Memory)
	}
}

func TestMetaNoteAndVersionAreCapped(t *testing.T) {
	if _, err := Export(dpDot(), nil, ExportOptions{Now: dpNow, Note: strings.Repeat("n", 2000), Version: strings.Repeat("v", 64)}); err != nil {
		t.Fatalf("at the cap: %v", err)
	}
	if _, err := Export(dpDot(), nil, ExportOptions{Now: dpNow, Note: strings.Repeat("n", 2001)}); err == nil {
		t.Fatal("export accepted a long note")
	}
	if _, err := Export(dpDot(), nil, ExportOptions{Now: dpNow, Version: strings.Repeat("v", 65)}); err == nil {
		t.Fatal("export accepted a long version")
	}
	base := dpPackWith(t, "")
	long := strings.Replace(base, `note = "n"`, `note = "`+strings.Repeat("n", 2001)+`"`, 1)
	if _, err := Parse([]byte(long)); !errors.Is(err, ErrFormat) {
		t.Fatalf("parse long note: %v", err)
	}
	long = strings.Replace(base, `herdr_dots_version = "0.9.0"`, `herdr_dots_version = "`+strings.Repeat("v", 65)+`"`, 1)
	if _, err := Parse([]byte(long)); !errors.Is(err, ErrFormat) {
		t.Fatalf("parse long version: %v", err)
	}
}

func TestExportRefusesAnInvalidDot(t *testing.T) {
	d := dpDot()
	d.Budget.MaxCallsPerSession = 0
	if _, err := Export(d, nil, ExportOptions{Now: dpNow}); err == nil {
		t.Fatal("exported an invalid dot")
	}
}

// exportRaw renders a dot without Export's checks, to build the hostile packs
// Prepare must still refuse.
func exportRaw(d dots.Dot) ([]byte, error) {
	return render(d, nil, ExportOptions{Now: dpNow})
}
