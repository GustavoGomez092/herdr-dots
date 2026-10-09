package config

import "slices"

// MCPTools are the Dots MCP server's tools (Phase 5), in display order.
var MCPTools = []string{"list_dots", "ask_dot", "ask_council", "list_queue", "read_memory", "propose_memory"}

// MCPConfig is [mcp] in config.toml.
type MCPConfig struct {
	Tools            []string `toml:"tools"`               // tools the server offers; [] = none
	MaxAsksPerTurn   int      `toml:"max_asks_per_turn"`   // per pane, reset when a turn ends
	MaxAsksPerMinute int      `toml:"max_asks_per_minute"` // per pane, sliding window
}

func DefaultMCP() MCPConfig {
	return MCPConfig{Tools: slices.Clone(MCPTools), MaxAsksPerTurn: 3, MaxAsksPerMinute: 6}
}

// Normalized fills unset limits with the defaults and clamps them (1–50 per
// turn, 1–120 per minute), and keeps only known tool names, once each, in
// MCPTools order. Tools == nil means every tool; an empty list means none.
func (m MCPConfig) Normalized() MCPConfig {
	d := DefaultMCP()
	if m.MaxAsksPerTurn <= 0 {
		m.MaxAsksPerTurn = d.MaxAsksPerTurn
	}
	if m.MaxAsksPerMinute <= 0 {
		m.MaxAsksPerMinute = d.MaxAsksPerMinute
	}
	m.MaxAsksPerTurn = min(m.MaxAsksPerTurn, 50)
	m.MaxAsksPerMinute = min(m.MaxAsksPerMinute, 120)
	if m.Tools == nil {
		m.Tools = d.Tools
		return m
	}
	out := []string{}
	for _, t := range MCPTools {
		if slices.Contains(m.Tools, t) {
			out = append(out, t)
		}
	}
	m.Tools = out
	return m
}
