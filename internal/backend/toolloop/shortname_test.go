package toolloop

import (
	"os"
	"path/filepath"
	"testing"
)

// NTFS opens ".env" through its 8.3 short name (ENV~1) without listing it.
// The exact on-disk-name rule must refuse that spelling: the directory listing
// holds ".env", never "ENV~1", so the component check fails before the
// deny-list would have to recognise the alias. The logic is OS-neutral, so it
// is pinned here on every OS; on a Windows volume with 8.3 names enabled the
// ResolveInRoot half exercises the real alias.
func TestShortNameAliasesAreNotExactOnDiskNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("API_KEY=x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newResolver()
	for _, alias := range []string{"ENV~1", "env~1", "PROGRA~1/x", "SECRET~1.JSO"} {
		if r.exact(root, alias) {
			t.Errorf("exact accepted the short-name spelling %q", alias)
		}
		if abs, _, err := ResolveInRoot(root, alias, DefaultDeny); err == nil {
			t.Errorf("%q resolved to %s", alias, abs)
		}
	}
	if !r.exact(root, ".env") {
		t.Fatal("exact must accept the real name")
	}
}
