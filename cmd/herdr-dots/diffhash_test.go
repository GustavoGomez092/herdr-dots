package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/patchstore"
)

// recEngine stands in for the engine socket: it records each request.
type recEngine struct {
	cmds []string
	args []json.RawMessage
}

func (r *recEngine) Call(_ context.Context, cmd string, args any) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	r.cmds, r.args = append(r.cmds, cmd), append(r.args, b)
	return nil
}

// The hash the viewer sends with an apply is the one the engine stored for
// the patch (and checks): built from a real patch store, raw bytes that
// display differently (an escape sequence, a tab) included.
func TestTheViewerSendsTheEnginesStoredPatchHash(t *testing.T) {
	store := patchstore.Open(filepath.Join(t.TempDir(), "patches"))
	const id = "0123456789abcdef"
	diff := []byte("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\t\x1b[2Jx\n")
	if err := store.PutPatch(patchstore.Meta{ID: id, Dot: "security", Card: "7", Stat: "1 file +1 −1", Paths: []string{"x.go"}, Status: patchstore.StatusReady}, diff); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(id)
	if err != nil || stored.SHA256 == "" {
		t.Fatalf("stored meta %+v %v", stored, err)
	}
	rec := &recEngine{}
	var m tea.Model = diffModel(store, id, "", false, rec)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y sent nothing")
	}
	cmd()
	if len(rec.cmds) != 1 || rec.cmds[0] != ipc.CmdApplyPatch {
		t.Fatalf("requests %v", rec.cmds)
	}
	var a ipc.CardArgs
	if err := json.Unmarshal(rec.args[0], &a); err != nil {
		t.Fatal(err)
	}
	if a.ID != "7" || a.SHA256 != stored.SHA256 {
		t.Fatalf("apply request %+v, want card 7 with the stored hash %s", a, stored.SHA256)
	}
}
