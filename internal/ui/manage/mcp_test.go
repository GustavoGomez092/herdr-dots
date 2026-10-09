package manage

import (
	"context"
	"strings"
	"testing"
)

func TestLibraryFooterShowsMCPInstallStatus(t *testing.T) {
	calls := 0
	d := Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil },
		MCPStatus: func() string { calls++; return "MCP: installed for Claude Code, Codex" }}
	m, _ := New("w1:p1", d).Update(SnapshotMsg(lib()))
	m = press(m, "j", "k")
	if v := m.View(); !strings.Contains(v, "MCP: installed for Claude Code, Codex") {
		t.Fatalf("view:\n%s", v)
	}
	if calls != 1 {
		t.Fatalf("MCPStatus read %d times; once when the popup opens", calls)
	}
}

func TestLibraryFooterWithoutMCPStatus(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	if strings.Contains(m.View(), "MCP:") {
		t.Fatal("no MCP line without Deps.MCPStatus")
	}
}
