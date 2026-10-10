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
