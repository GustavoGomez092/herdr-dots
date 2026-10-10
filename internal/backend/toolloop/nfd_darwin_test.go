package toolloop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// G4: on macOS git precomposes the names it reads from disk
// (core.precomposeunicode, true by default there), so a file whose name is
// stored decomposed (NFD) was listed under its NFC name, which the exact
// on-disk name check then dropped: the file vanished from list_files. With
// precomposition off for the listing, git reports the on-disk name.
func TestListShowsAFileWithADecomposedName(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	if out, err := exec.Command("git", "-C", root, "config", "core.precomposeunicode", "true").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	nfd := "café.go" // "café.go", decomposed
	if err := os.WriteFile(filepath.Join(root, nfd), []byte("package cafe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	stored := false
	for _, e := range ents {
		stored = stored || e.Name() == nfd
	}
	if !stored {
		t.Skip("this file system does not keep the decomposed name")
	}
	if out, err := exec.Command("git", "-C", root, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	out, err := run(t, RepoTools(root, DefaultLimits()), "list_files", map[string]any{})
	if err != nil || !strings.Contains(out, nfd) {
		t.Fatalf("list_files lost the decomposed name:\n%q\n%v", out, err)
	}
}
