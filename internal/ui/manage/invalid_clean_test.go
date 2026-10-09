package manage

import (
	"context"
	"strings"
	"testing"

	"herdr-dots/internal/view"
)

// Phase 9 fix: an invalid entry's file name and error come from a
// repository; control characters never reach the terminal (the probe is an
// OSC 52 clipboard write in a project dot file name).
func TestInvalidEntriesAreCleaned(t *testing.T) {
	snap := lib()
	snap.Invalid = []view.Invalid{{File: "app: .herdr-dots/dots/x\x1b]52;c;aGk=\a.toml", Line: 1, Error: "bad \x1b[2J name"}}
	m, _ := New("w1:p1", Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil }}).Update(SnapshotMsg(snap))
	v := m.View()
	if strings.Contains(v, "\x1b]") || strings.Contains(v, "\a") || strings.Contains(v, "\x1b[2J") {
		t.Fatalf("raw escapes in the view: %q", v)
	}
	if !strings.Contains(v, "x]52;c;aGk=.toml") {
		t.Fatalf("the cleaned name is shown:\n%s", v)
	}
}
