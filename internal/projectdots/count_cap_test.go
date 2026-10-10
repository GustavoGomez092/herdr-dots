package projectdots

import (
	"fmt"
	"strings"
	"testing"
)

// H11: a project file with more keys and tables than any dot needs is
// refused before the TOML decoder sees it (the pre-scan bounds nesting and
// key paths, not how many there are).
func TestAFileWithTooManyKeysOrTablesIsRefusedBeforeDecoding(t *testing.T) {
	var b strings.Builder
	b.WriteString(helperTOML)
	for i := 0; i <= MaxKeysAndTables; i++ {
		fmt.Fprintf(&b, "[t%d]\n", i)
	}
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML, "bomb.toml": b.String()})
	s := ScanProject(root, pdProject, pdTrust(t))
	if len(s.Items) != 1 || s.Items[0].Rel != ".herdr-dots/dots/helper.toml" {
		t.Fatalf("items %+v", s.Items)
	}
	if len(s.Invalid) != 1 || s.Invalid[0].File != ".herdr-dots/dots/bomb.toml" || !strings.Contains(s.Invalid[0].Err.Error(), "keys or tables") {
		t.Fatalf("invalid %+v", s.Invalid)
	}
	if err := CheckCounts([]byte(helperTOML)); err != nil {
		t.Fatalf("an ordinary dot: %v", err)
	}
}

// H11: '=' and '[' inside a string (code or Markdown in a prompt) are not
// keys or tables, so a real dot holding thousands of them still loads.
func TestCodeInAPromptStringIsNotCountedAsKeysOrTables(t *testing.T) {
	code := strings.Repeat("a[i] = b[j]; ", 1700) // 5,100 '=' and '[' in all
	body := `name = "Coder"
glyph = "c"
color = "cyan"
purpose = """
` + code + `"""

[delivery]
mode = "inject"
`
	root := pdRepo(t, map[string]string{"coder.toml": body})
	s := ScanProject(root, pdProject, pdTrust(t))
	if len(s.Invalid) != 0 || len(s.Items) != 1 {
		t.Fatalf("items %+v invalid %+v", s.Items, s.Invalid)
	}
	if err := CheckCounts([]byte(body)); err != nil {
		t.Fatalf("CheckCounts: %v", err)
	}
	var bomb strings.Builder
	bomb.WriteString(body)
	for i := 0; i <= MaxKeysAndTables; i++ {
		fmt.Fprintf(&bomb, "k%d = 1\n", i)
	}
	if err := CheckCounts([]byte(bomb.String())); err == nil || !strings.Contains(err.Error(), "keys or tables") {
		t.Fatalf("a real key bomb after the strings: err = %v", err)
	}
}
