package manage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
	"herdr-dots/internal/view"
)

// U19 (post-rc hardening): M on an empty library says why nothing opened and
// points at P (project memory) instead of doing nothing.
func TestMemoryKeyOnAnEmptyLibrarySaysWhy(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	m, _ := New("w1:p1", Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil },
		Memory: st}).Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1"}}}))
	v := press(m, "M").View()
	if !strings.Contains(v, "no dot selected") || !strings.Contains(v, "P project memory") {
		t.Fatalf("M on an empty library:\n%s", v)
	}
}
