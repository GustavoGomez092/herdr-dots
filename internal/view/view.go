// Package view holds the snapshot the engine streams to the UIs.
package view

import (
	"herdr-dots/internal/council"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

const (
	StatusIdle     = "idle"
	StatusThinking = "thinking"
	StatusPassed   = "passed"
	StatusFlagged  = "flagged"
	StatusError    = "error"
	StatusBudget   = "budget"
	StatusPaused   = "paused"
	// StatusConsulted: the main agent asked this dot through the MCP server;
	// it skips the turn it was asked in (Phase 5).
	StatusConsulted = "consulted"
)

type DotState struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Glyph  string `json:"glyph"`
	Color  string `json:"color"`
	Mode   string `json:"mode"`
	Status string `json:"status"`
	Paused bool   `json:"paused"`
	Calls  int    `json:"calls"`
	Budget int    `json:"budget"`

	MemoryEntries int  `json:"memory_entries"` // this dot's entries for the pane's project
	Learning      bool `json:"learning"`       // a memory pass is running

	CostUSD     float64 `json:"cost_usd,omitempty"`
	CostUnknown bool    `json:"cost_unknown,omitempty"` // some runs had no price
	BudgetUSD   float64 `json:"budget_usd,omitempty"`   // 0 = off
}

type Pane struct {
	PaneID string            `json:"pane_id"`
	Agent  string            `json:"agent"`
	Source string            `json:"source"`
	Calls  int               `json:"calls"`
	Budget int               `json:"budget"`
	Dots   []DotState        `json:"dots"`
	Cards  []queue.Card      `json:"cards"`
	Feed   []state.FeedEntry `json:"feed"`

	ProjectID       string `json:"project_id,omitempty"`
	ProjectName     string `json:"project_name,omitempty"`
	MemoryEntries   int    `json:"memory_entries"`   // project + dot entries for this project
	MemoryProposals int    `json:"memory_proposals"` // project entries awaiting approval

	CostUSD     float64 `json:"cost_usd,omitempty"`     // known spend on this pane, council chair included
	CostUnknown bool    `json:"cost_unknown,omitempty"` // some run on this pane (a dot's or the chair's) had no price
	BudgetUSD   float64 `json:"budget_usd,omitempty"`   // 0 = off

	Council          *CouncilState      `json:"council,omitempty"`           // a council in progress
	CouncilPreflight *council.Preflight `json:"council_preflight,omitempty"` // the last preflight, for the popup
}

// Council phases (CouncilState.Phase).
const (
	CouncilMembers = "members" // round 1 runs
	CouncilRound2  = "round2"  // dot-to-dot cross-review runs
	CouncilMerging = "merging" // the chair (or the plain merge) builds the digest
)

// CouncilState is a council in progress on a pane.
type CouncilState struct {
	ID    string `json:"id"`
	Phase string `json:"phase"`
	Done  int    `json:"done"`  // members finished in this round
	Total int    `json:"total"` // members running in this round
	Round int    `json:"round"`
}

type LibraryDot struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Glyph   string `json:"glyph"`
	Color   string `json:"color"`
	Backend string `json:"backend"`
	Model   string `json:"model"`
	Mode    string `json:"mode"`
}

type Invalid struct {
	File  string `json:"file"`
	Line  int    `json:"line"`
	Error string `json:"error"`
}

type Snapshot struct {
	Panes         []Pane       `json:"panes"`
	Library       []LibraryDot `json:"library"`
	Invalid       []Invalid    `json:"invalid"`
	InjectEnabled bool         `json:"inject_enabled"`
	PausedAll     bool         `json:"paused_all"`
}

func (s Snapshot) Pane(id string) (Pane, bool) {
	for _, p := range s.Panes {
		if p.PaneID == id {
			return p, true
		}
	}
	return Pane{}, false
}
