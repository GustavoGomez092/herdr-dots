package ipc

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
)

func TestDraftCommandsRoundTrip(t *testing.T) {
	h, c := serve(t)
	want := DraftArgs{PaneID: "w1:p1", Slug: "security", FromCardID: "7"}
	if err := c.Call(context.Background(), CmdDraft, want); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), CmdApplyPatch, CardArgs{ID: "9"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), CmdDiscardPatch, CardArgs{ID: "9"}); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !slices.Equal(h.got, []string{"draft", "apply_patch", "discard_patch"}) {
		t.Fatalf("commands %v", h.got)
	}
	var got DraftArgs
	if err := json.Unmarshal(h.args[0], &got); err != nil || got != want {
		t.Fatalf("args %s: %+v %v", h.args[0], got, err)
	}
	if string(h.args[0]) != `{"pane_id":"w1:p1","slug":"security","from_card_id":"7"}` {
		t.Fatalf("an empty instruction is omitted: %s", h.args[0])
	}
}

// The MCP server (Phase 5) can only send QueryCmds: no write command may
// ever be one, so the main agent can never draft or apply a patch.
func TestWriteCommandsAreNeverQueries(t *testing.T) {
	for _, cmd := range []string{CmdDraft, CmdApplyPatch, CmdDiscardPatch} {
		if slices.Contains(QueryCmds, cmd) {
			t.Errorf("%s must not be a query command", cmd)
		}
	}
}
