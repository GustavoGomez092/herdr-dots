package mcpserver

import (
	"errors"
	"fmt"
	"testing"

	"herdr-dots/internal/ipc"
)

// Phase 5 final review M6: a closed pane reads as such to the agent, whether
// the engine's error came over the socket (plain text) or as the sentinel.
func TestPaneClosedErrorsBecomeClearText(t *testing.T) {
	const want = "the pane was closed; the dot can no longer be asked"
	for _, c := range []struct {
		tool string
		args map[string]any
		err  error
	}{
		{"ask_dot", map[string]any{"slug": "security", "question": "q"}, errors.New("pane closed")},
		{"ask_dot", map[string]any{"slug": "security", "question": "q"}, ipc.ErrPaneClosed},
		{"ask_council", map[string]any{"question": "q"}, errors.New("council ended without an answer: pane closed")},
		{"ask_council", map[string]any{"question": "q"}, fmt.Errorf("council ended without an answer: %w", ipc.ErrPaneClosed)},
	} {
		fe := &fakeEngine{err: c.err}
		if text, _, isErr := call(t, connect(t, inPane(fe)), c.tool, c.args); !isErr || text != want {
			t.Errorf("%s with %q: isError=%v text=%q", c.tool, c.err, isErr, text)
		}
	}
	// Only a whole "pane closed" error counts: a dot's failure text that
	// merely mentions it passes through as it is.
	fe := &fakeEngine{err: errors.New("Security failed: the pane closed button is broken")}
	if text, _, _ := call(t, connect(t, inPane(fe)), "ask_dot", map[string]any{"slug": "security", "question": "q"}); text == want {
		t.Fatalf("text %q", text)
	}
}

// Phase 5 final review I2: propose_memory refuses once 20 agent proposals
// wait for the developer, and says why.
func TestProposeMemoryRefusesTheTwentyFirstWaitingProposal(t *testing.T) {
	fe := &fakeEngine{dots: ipc.DotsResult{ProjectID: testPID}}
	d := inPane(fe)
	d.Memory = memStore(t)
	cs := connect(t, d)
	for i := range 20 {
		text := fmt.Sprintf("word%dx kilo%dy lima%dz", i, i, i)
		if out, _, isErr := call(t, cs, "propose_memory", map[string]any{"kind": "note", "text": text}); isErr {
			t.Fatalf("proposal %d: %s", i+1, out)
		}
	}
	text, _, isErr := call(t, cs, "propose_memory", map[string]any{"kind": "note", "text": "one more distinct proposal"})
	if !isErr || text != "20 proposals are waiting for the developer; approve or reject some first" {
		t.Fatalf("isError=%v text=%q", isErr, text)
	}
}
