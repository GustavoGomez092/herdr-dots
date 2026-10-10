package state

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// Fix round 1: a loose feed that rotates at open becomes owner-only too, and
// so does an older loose feed.jsonl.1.
func TestOpenFeedMakesRotatedFeedsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits; Windows privacy is the session dir's DACL")
	}
	for _, name := range []string{"rotated now", "rotated before"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "feed.jsonl")
			body := []byte(`{"pane_id":"w1:p1","dot":"security","outcome":"passed"}` + "\n")
			if name == "rotated now" {
				body = append(body, make([]byte, feedRotateBytes)...)
				if err := os.WriteFile(p, body, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(p, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(p+".1", body, 0o644); err != nil {
				t.Fatal(err)
			} else if err := os.Chmod(p+".1", 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenFeed(p); err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(p + ".1")
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm() != 0o600 {
				t.Fatalf("rotated feed mode %v, want 0600", st.Mode().Perm())
			}
		})
	}
}

// A chmod that fails is logged, once per open, not silently skipped.
func TestOpenFeedLogsAFailedChmodOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits; Windows privacy is the session dir's DACL")
	}
	p := filepath.Join(t.TempDir(), "feed.jsonl")
	for _, f := range []string{p, p + ".1"} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(f, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := chmod
	chmod = func(string, os.FileMode) error { return os.ErrPermission }
	t.Cleanup(func() { chmod = old })
	var logs []string
	if _, err := OpenFeedLogged(p, func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "owner-only") {
		t.Fatalf("want one log line, got %q", logs)
	}
}
