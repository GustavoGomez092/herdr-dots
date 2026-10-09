package toolloop

import (
	"testing"
)

// Final review item 12: a dot that asked for repo access but gets no tools
// (no working directory, or one too broad) is told so.
func TestNoteForRepoAccess(t *testing.T) {
	proj := t.TempDir()
	cases := []struct {
		want  bool
		cwd   string
		tools bool
		note  string
	}{
		{true, proj, true, SystemNote},
		{true, "", false, NoToolsNote},
		{true, "/", false, NoToolsNote},
		{false, "", false, ""},
		{false, "/", false, ""},
		{true, proj, false, ""}, // tools withheld for another reason: no note
	}
	for _, c := range cases {
		if got := Note(c.want, c.cwd, c.tools); got != c.note {
			t.Errorf("Note(%v, %q, %v) = %q, want %q", c.want, c.cwd, c.tools, got, c.note)
		}
	}
}
