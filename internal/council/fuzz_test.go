package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzParseDigest(f *testing.F) {
	files, _ := filepath.Glob("testdata/chair/*.txt")
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("## Must fix\n- Token logged (Security)\n## Not heard from\n- Frontend")
	f.Add("## Should fix\n- unattributed point")
	f.Add("- (")
	f.Fuzz(func(t *testing.T, message string) {
		body, verified := ParseDigest(message)
		if strings.IndexFunc(body, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }) >= 0 {
			t.Fatalf("digest body not sanitized: %q", body)
		}
		if verified && !strings.Contains(body, "## ") {
			t.Fatalf("verified digest without a section header: %q", body)
		}
	})
}

func FuzzParseStance(f *testing.F) {
	for _, s := range []string{"AGREE: yes", "**OBJECT:** no", "add: more", "ADD", "", "ÄGREE: x", "`AGREE:`"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, message string) {
		stance, rest := ParseStance(message)
		switch stance {
		case "":
			if rest != strings.TrimSpace(message) {
				t.Fatalf("no stance must keep the message: %q -> %q", message, rest)
			}
		case StanceAgree, StanceObject, StanceAdd:
			if utf8.RuneCountInString(rest) > utf8.RuneCountInString(message) {
				t.Fatalf("rest longer than the message: %q -> %q", message, rest)
			}
		default:
			t.Fatalf("unknown stance %q", stance)
		}
	})
}
