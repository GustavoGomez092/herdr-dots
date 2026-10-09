package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenFeedMakesAnOldLooseFileOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits; Windows privacy is the session dir's DACL")
	}
	p := filepath.Join(t.TempDir(), "feed.jsonl")
	if err := os.WriteFile(p, []byte(`{"pane_id":"w1:p1","dot":"security","outcome":"passed"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil { // umask may have narrowed it
		t.Fatal(err)
	}
	f, err := OpenFeed(p)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("feed mode %v, want 0600", st.Mode().Perm())
	}
	if got := f.Recent("w1:p1", 0); got != nil {
		t.Fatalf("Recent(0) = %v", got)
	}
	if got := f.Recent("w1:p1", -1); got != nil {
		t.Fatalf("Recent(-1) = %v", got)
	}
	if got := f.Recent("w1:p1", 5); len(got) != 1 {
		t.Fatalf("Recent(5) = %v", got)
	}
}
