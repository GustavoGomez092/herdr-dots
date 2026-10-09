package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV1AttachmentsFileLoadsWithoutMemoryFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "attachments.json")
	v1 := `{"w1:p1":{"pane_id":"w1:p1","agent":"claude","session_kind":"id","session_value":"s1","cwd":"/repo","source":"claude","force_scrollback":false,"dots":[{"slug":"security","last_seen_turn":4,"paused":false,"cooldown_left":0,"failures":0}]}}`
	os.WriteFile(p, []byte(v1), 0o600)
	s, err := OpenAttachments(p)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := s.Get("w1:p1")
	if !ok || a.ProjectID != "" || a.Find("security").MemorySeenTurn != 0 || a.Find("security").LastSeenTurn != 4 {
		t.Fatalf("%+v", a)
	}
}

func TestMemoryFieldsPersist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "attachments.json")
	s, _ := OpenAttachments(p)
	_ = s.Put(Attachment{PaneID: "w1:p1", ProjectID: "g-0123456789abcdef", ProjectName: "herdr-dots",
		Dots: []DotAttachment{{Slug: "copy", MemorySeenTurn: 5, MemoryPasses: 2}}})
	s2, _ := OpenAttachments(p)
	a, _ := s2.Get("w1:p1")
	if a.ProjectID != "g-0123456789abcdef" || a.ProjectName != "herdr-dots" || a.Find("copy").MemorySeenTurn != 5 || a.Find("copy").MemoryPasses != 2 {
		t.Fatalf("%+v", a)
	}
}

func TestMemoryFeedOutcomes(t *testing.T) {
	if OutcomeLearned != "learned" || OutcomeSuppressed != "suppressed" {
		t.Fatal("feed outcome names are part of the UI contract")
	}
}
