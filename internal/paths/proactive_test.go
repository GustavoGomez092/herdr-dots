package paths

import (
	"path/filepath"
	"testing"
)

func TestProactiveFilesAreSharedAcrossSessions(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	a := New(filepath.Join(state, "..", "cfg"), state, "/tmp/herdr-a.sock")
	b := New(filepath.Join(state, "..", "cfg"), state, "/tmp/herdr-b.sock")
	if a.DailyFile() != filepath.Join(state, "daily.json") || a.InboxFile() != filepath.Join(state, "inbox.json") {
		t.Fatalf("daily %s inbox %s", a.DailyFile(), a.InboxFile())
	}
	if a.DailyFile() != b.DailyFile() || a.InboxFile() != b.InboxFile() || a.QueueFile() == b.QueueFile() {
		t.Fatal("daily.json and inbox.json are shared by every session; the queue is not")
	}
}
