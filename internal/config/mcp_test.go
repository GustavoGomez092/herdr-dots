package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeMCPConfig(t *testing.T, body string) Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMCPDefaults(t *testing.T) {
	c := Default()
	if !slices.Equal(c.MCP.Tools, MCPTools) || c.MCP.MaxAsksPerTurn != 3 || c.MCP.MaxAsksPerMinute != 6 {
		t.Fatalf("defaults %+v", c.MCP)
	}
	c.MCP.Tools[0] = "changed"
	if MCPTools[0] != "list_dots" {
		t.Fatal("Default must copy MCPTools, not share it")
	}
}

func TestMCPLoadKeepsKnownToolsAndClamps(t *testing.T) {
	c := writeMCPConfig(t, "[mcp]\ntools = [\"read_memory\", \"ask_dot\", \"bogus\", \"ask_dot\"]\nmax_asks_per_turn = 0\nmax_asks_per_minute = 500\n")
	if !slices.Equal(c.MCP.Tools, []string{"ask_dot", "read_memory"}) {
		t.Fatalf("tools %v", c.MCP.Tools)
	}
	if c.MCP.MaxAsksPerTurn != 3 || c.MCP.MaxAsksPerMinute != 120 {
		t.Fatalf("limits %+v", c.MCP)
	}
}

func TestMCPEmptyToolListMeansNoTools(t *testing.T) {
	c := writeMCPConfig(t, "[mcp]\ntools = []\n")
	if c.MCP.Tools == nil || len(c.MCP.Tools) != 0 {
		t.Fatalf("tools %#v: an explicit empty list offers no tools", c.MCP.Tools)
	}
}

func TestMCPMissingTableKeepsDefaults(t *testing.T) {
	c := writeMCPConfig(t, "inject_enabled = false\n")
	if !slices.Equal(c.MCP.Tools, MCPTools) || c.MCP.MaxAsksPerTurn != 3 || c.MCP.MaxAsksPerMinute != 6 {
		t.Fatalf("mcp %+v", c.MCP)
	}
}

func TestMCPNormalizedNilToolsMeansAll(t *testing.T) {
	m := MCPConfig{MaxAsksPerTurn: 2}.Normalized()
	if !slices.Equal(m.Tools, MCPTools) || m.MaxAsksPerTurn != 2 || m.MaxAsksPerMinute != 6 {
		t.Fatalf("normalized %+v", m)
	}
}
