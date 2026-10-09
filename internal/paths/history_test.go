package paths

import (
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/platform/platformtest"
)

// Phase 9: history is shared by every Herdr session (like memory); trust is
// user configuration.
func TestHistoryAndTrustPaths(t *testing.T) {
	p := New("/c", "/s", "/sock")
	if p.HistoryDir() != filepath.Join("/s", "history") {
		t.Fatalf("history dir %q", p.HistoryDir())
	}
	if p.TrustFile() != filepath.Join("/c", "trusted.json") {
		t.Fatalf("trust file %q", p.TrustFile())
	}
}

func TestEnsureCreatesThePrivateHistoryDir(t *testing.T) {
	root := t.TempDir()
	p := New(filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "herdr.sock"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p.HistoryDir())
	if err != nil || !st.IsDir() {
		t.Fatalf("history dir: %v", err)
	}
	platformtest.AssertPrivateDir(t, p.HistoryDir())
}
