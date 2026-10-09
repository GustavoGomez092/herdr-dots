// Package view holds the snapshot the engine streams to the UIs.
package view

import (
	"time"

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

	Next       string `json:"next,omitempty"`       // proactive hint: "research in 6m", "scheduled Mon 09:00", "paused · daily budget"
	Background string `json:"background,omitempty"` // a background run in flight: "research", "scheduled" or "commit"

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

	Away *AwayState `json:"away,omitempty"` // research results waiting for the developer to come back

	Drafts []DraftState `json:"drafts,omitempty"` // drafts running for this pane (Phase 7)
}

// Draft phases (DraftState.Phase).
const (
	DraftSnapshot   = "snapshot"   // snapshotting the working tree and checking out the worktree
	DraftRunning    = "running"    // the dot is editing its isolated copy
	DraftValidating = "validating" // capturing and validating the patch
)

// DraftState is one draft in progress.
type DraftState struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Phase     string    `json:"phase"`
	StartedAt time.Time `json:"started_at"`
}

// AwayState is research collected while the developer was away, not yet a
// digest card (it becomes one on the next turn, focus or critical result).
type AwayState struct {
	Since   time.Time `json:"since"`   // the idle period's start
	Pending int       `json:"pending"` // relevant research results collected
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

	Write               bool `json:"write,omitempty"`                 // [capabilities] write (Phase 7)
	WriteTimeoutSeconds int  `json:"write_timeout_seconds,omitempty"` // shown in the draft confirm

	Origin      string   `json:"origin,omitempty"`       // Phase 9: user, template, import or project
	Trusted     bool     `json:"trusted,omitempty"`      // project dots: this exact file is trusted
	Clamped     []string `json:"clamped,omitempty"`      // project dots: capabilities turned off now
	Requested   []string `json:"requested,omitempty"`    // project dots: capabilities the file asks for
	ProjectID   string   `json:"project_id,omitempty"`   // project dots: the repository's project id
	ProjectName string   `json:"project_name,omitempty"` // project dots: the repository's name
	Path        string   `json:"path,omitempty"`         // project dots: the file (absolute; for reading, not for display)
	SHA256      string   `json:"sha256,omitempty"`       // project dots: hex hash of the file as scanned
	Rel         string   `json:"rel,omitempty"`          // project dots: the file from the repository root (for display)
	TrustedAt   string   `json:"trusted_at,omitempty"`   // project dots: where trust was given when it was another checkout ("~"-abbreviated); "" = here
	Root        string   `json:"root,omitempty"`         // project dots: the checkout this entry is from; a project dot is (Root, Slug) (ruling P9-R13)
	Purpose     string   `json:"purpose,omitempty"`      // project dots: the purpose of the trusted copy that runs; "" while untrusted
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
	WriteEnabled  bool         `json:"write_enabled"` // config write_enabled (Phase 7)

	// Background work today, every session (Phase 8; the pane header shows it
	// once TodayRuns > 0).
	TodayUSD       float64 `json:"today_usd"`
	TodayRuns      int     `json:"today_runs"`
	DailyBudgetUSD float64 `json:"daily_budget_usd"`

	PaneProjects map[string]string `json:"pane_projects,omitempty"` // Phase 9: pane id → project id of its scanned repository
	PaneRoots    map[string]string `json:"pane_roots,omitempty"`    // Phase 9: pane id → its checkout's root (match LibraryDot.Root)
}

func (s Snapshot) Pane(id string) (Pane, bool) {
	for _, p := range s.Panes {
		if p.PaneID == id {
			return p, true
		}
	}
	return Pane{}, false
}
