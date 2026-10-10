package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Q4: a feed whose last line was cut short (a crash mid-write) says so in
// the log, and the next entry starts on its own line instead of being glued
// onto the damaged one (and lost with it on the next load).
func TestFeedLogsATruncatedLastLineAndAppendsAfterIt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "feed.jsonl")
	body := `{"pane_id":"w1:p1","dot":"a","outcome":"pass"}` + "\n" + `{"pane_id":"w1:p1","dot":"b","outc`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	f, err := OpenFeedLogged(p, logf)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "1 unreadable line") || !strings.Contains(logs[0], p) {
		t.Fatalf("logs = %q, want one line naming the feed and 1 unreadable line", logs)
	}
	if err := f.Append(FeedEntry{PaneID: "w1:p1", Dot: "c", Outcome: OutcomePass}); err != nil {
		t.Fatal(err)
	}
	g, err := OpenFeed(p)
	if err != nil {
		t.Fatal(err)
	}
	got := g.Recent("w1:p1", 10)
	if len(got) != 2 || got[0].Dot != "a" || got[1].Dot != "c" {
		t.Fatalf("after reload: %+v, want dots a and c", got)
	}
}

// Q6: a rotation that fails is logged; the feed keeps appending to the
// current file.
func TestFeedRotationFailureIsLogged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "feed.jsonl")
	if err := os.WriteFile(p, make([]byte, feedRotateBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory where the rotated file goes: the rename fails.
	if err := os.MkdirAll(filepath.Join(p+".1", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	var logs []string
	if _, err := OpenFeedLogged(p, func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }); err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 || !strings.Contains(strings.Join(logs, "\n"), "rotate") {
		t.Fatalf("logs = %q, want the failed rotation", logs)
	}
}

func TestFeedLoadOfAWholeFileLogsNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "feed.jsonl")
	if err := os.WriteFile(p, []byte(`{"pane_id":"w1:p1","dot":"a","outcome":"pass"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs []string
	f, err := OpenFeedLogged(p, func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) })
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("logs = %q", logs)
	}
	if err := f.Append(FeedEntry{PaneID: "w1:p1", Dot: "b", Outcome: OutcomePass}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if strings.Count(string(data), "\n") != 2 || strings.Contains(string(data), "\n\n") {
		t.Fatalf("feed = %q, want two lines and no blank one", data)
	}
}
