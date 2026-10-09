package toolloop

import (
	"os"
	"path/filepath"
	"testing"
)

// Final review item 3: a root that reaches home through a path suffix (a
// firmlink like /System/Volumes/Data → /Users/u, or a bind mount) gets no tools.
func TestRootReachingHomeThroughASuffixIsUnavailable(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "Users", "u")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(tmp, "System", "Volumes", "Data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	// data/Users is the same directory as tmp/Users (a firmlink stand-in).
	symlinkOrSkip(t, filepath.Join(tmp, "Users"), filepath.Join(data, "Users"))
	setHome(t, home)
	if !Unavailable(data) {
		t.Fatal("a root that reaches home via data/Users/u must be unavailable")
	}
	if got := RepoTools(data, DefaultLimits()); len(got) != 0 {
		t.Fatalf("RepoTools gave %d tools", len(got))
	}
	// A project that merely has a directory named like the user is fine.
	proj := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(filepath.Join(proj, "u"), 0o700); err != nil {
		t.Fatal(err)
	}
	if Unavailable(proj) {
		t.Fatal("an ordinary project must keep its tools")
	}
}

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
