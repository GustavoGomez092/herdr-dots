package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
)

// S16 (post-rc hardening): global entries are a dot's memory across every
// project. read_memory returns them only for the dots attached to the pane,
// so an agent in one project can't read what an unattached dot learned in
// another.
func TestReadMemoryLeavesOutGlobalEntriesOfUnattachedDots(t *testing.T) {
	st := memStore(t)
	for _, e := range []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindConvention, Text: "Use pnpm, never npm."},
		{Scope: memory.ScopeGlobal, Dot: "security", Kind: memory.KindPreference, Text: "Prefer short findings."},
		{Scope: memory.ScopeGlobal, Dot: "perf", Kind: memory.KindPreference, Text: "Client Orion's billing service is slow."},
	} {
		if _, err := st.Add(testPID, e); err != nil {
			t.Fatal(err)
		}
	}
	d := inPane(&fakeEngine{dots: ipc.DotsResult{ProjectID: testPID, Dots: []ipc.DotInfo{{Slug: "security"}}}})
	d.Memory = st
	cs := connect(t, d)
	for _, args := range []map[string]any{{}, {"dot": "perf"}} {
		text, js, isErr := call(t, cs, "read_memory", args)
		if isErr {
			t.Fatal(text)
		}
		var out ReadMemoryOut
		_ = json.Unmarshal(js, &out)
		if strings.Contains(text, "Orion") || strings.Contains(string(js), "Orion") {
			t.Fatalf("an unattached dot's global entry leaked (args %v): %s", args, js)
		}
		if len(args) == 0 && (out.Total != 2 || len(out.Entries) != 2 || !strings.Contains(text, "Prefer short findings")) {
			t.Fatalf("the attached dot's global entry and the project entry stay: %s", js)
		}
	}
}
