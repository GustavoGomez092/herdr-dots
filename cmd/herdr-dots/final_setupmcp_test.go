package main

import (
	"os"
	"strings"
	"testing"

	"herdr-dots/internal/mcpinstall"
)

// Phase 5 final review M8: when the change went through but the install
// record can't be saved (a corrupt mcp-install.json), the error says the
// change was applied, so nobody re-runs or undoes it by hand.
func TestSetupMCPSaysTheChangeWasAppliedWhenOnlyTheRecordFails(t *testing.T) {
	r := newMCPSetupRig(t, "codex")
	rec := mcpinstall.RecordPath(r.p.ConfigDir)
	if err := os.WriteFile(rec, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := setupMCP([]string{"--client", "codex", "--yes"}, r.io)
	if err == nil || !strings.Contains(err.Error(), "the change was applied; only the install record could not be saved ("+rec+")") {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(r.codexConfig()); !strings.Contains(string(b), "[mcp_servers.herdr-dots]") {
		t.Fatalf("the Codex change is missing:\n%s", b)
	}
	// Nothing to change: the record failure is still named as only that.
	err = setupMCP([]string{"--client", "codex", "--yes"}, r.io)
	if err == nil || !strings.Contains(err.Error(), "only the install record could not be saved ("+rec+")") {
		t.Fatalf("nothing to change: %v", err)
	}
}
