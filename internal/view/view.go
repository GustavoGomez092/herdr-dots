// Package view holds the snapshot the engine streams to the UIs.
package view

import (
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
