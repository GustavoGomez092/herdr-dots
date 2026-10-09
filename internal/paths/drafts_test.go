package paths

import (
	"path/filepath"
	"testing"
)

func TestDraftDirsLiveUnderTheSessionDir(t *testing.T) {
	root := t.TempDir()
	p := New(filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "herdr.sock"))
	if p.WorktreesDir() != filepath.Join(p.SessionDir, "worktrees") || p.PatchesDir() != filepath.Join(p.SessionDir, "patches") {
		t.Fatalf("worktrees %s patches %s", p.WorktreesDir(), p.PatchesDir())
	}
}
