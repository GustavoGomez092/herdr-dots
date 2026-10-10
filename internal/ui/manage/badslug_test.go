package manage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/view"
)

// Fix round 1: a library dot whose file name is not a safe slug (a
// hand-named "a:b.toml") can't be edited or deleted through dots.Load or
// dots.Delete; the popup says so on a readable status line naming the file.
func TestARefusedSlugShowsAReadableStatus(t *testing.T) {
	dir := t.TempDir()
	odd := filepath.Join(dir, "a:b.toml")
	if err := os.WriteFile(odd, []byte("name = \"Odd\"\npurpose = \"p\"\n"), 0o600); err != nil {
		t.Skipf("this file system refuses the name: %v", err)
	}
	snap := view.Snapshot{Library: []view.LibraryDot{{Slug: "a:b", Name: "Odd", Glyph: "o", Backend: "claude-cli", Model: "sonnet", Mode: "queue"}}}
	for _, keys := range [][]string{{"e"}, {"D", "y"}} {
		var m tea.Model
		m, _ = New("w1:p1", Deps{Caller: &fakeCaller{}, DotsDir: dir, Check: func(context.Context, string) error { return nil }}).Update(SnapshotMsg(snap))
		v := press(m, keys...).View()
		if !strings.Contains(v, `invalid dot name: "a:b.toml"`) {
			t.Errorf("%v: want a status naming the refused file:\n%s", keys, v)
		}
		if _, err := os.Stat(odd); err != nil {
			t.Fatalf("%v: the file must stay: %v", keys, err)
		}
	}
}
