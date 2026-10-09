package mcpserver

import (
	"fmt"
	"testing"

	"herdr-dots/internal/ipc"
)

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
