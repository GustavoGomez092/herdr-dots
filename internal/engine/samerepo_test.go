package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The pane's repository matches the patch's by file identity: a symlinked
// path or (on a case-insensitive file system) a case variant is the same
// repository; another directory never is.
func TestSameRepoComparesFileIdentity(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "Repo")
	other := filepath.Join(base, "other")
	for _, d := range []string{repo, other} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !sameRepo(link, repo) {
		t.Error("a symlinked path is the same repository")
	}
	if sameRepo(other, repo) {
		t.Error("another directory matched")
	}
	variant := filepath.Join(base, strings.ToLower("Repo"))
	if _, err := os.Stat(variant); err == nil { // a case-insensitive file system
		if !sameRepo(variant, repo) {
			t.Error("a case variant of the same directory did not match")
		}
	}
	if sameRepo(filepath.Join(base, "gone"), repo) {
		t.Error("a missing path matched")
	}
}
