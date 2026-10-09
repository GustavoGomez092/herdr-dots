// Package herdr is a typed client for the subset of Herdr's socket API that
// herdr-dots uses. Field names match `herdr api schema`.
package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type AgentStatus string

const (
	StatusIdle    AgentStatus = "idle"
	StatusWorking AgentStatus = "working"
	StatusBlocked AgentStatus = "blocked"
	StatusDone    AgentStatus = "done"
	StatusUnknown AgentStatus = "unknown"
)

// AgentSession is reported by Herdr's agent integrations. Kind is "id" (a
// session id) or "path" (a transcript path).
type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type PaneInfo struct {
	PaneID        string        `json:"pane_id"`
	TabID         string        `json:"tab_id"`
	WorkspaceID   string        `json:"workspace_id"`
	Focused       bool          `json:"focused"`
	AgentStatus   AgentStatus   `json:"agent_status"`
	Agent         string        `json:"agent"`
	Cwd           string        `json:"cwd"`
	ForegroundCwd string        `json:"foreground_cwd"`
	AgentSession  *AgentSession `json:"agent_session"`
}

// WorkDir is the directory the agent is working in.
func (p PaneInfo) WorkDir() string {
	if p.ForegroundCwd != "" {
		return p.ForegroundCwd
	}
	return p.Cwd
}

type StatusEvent struct {
	PaneID string
	Status AgentStatus
	Agent  string
}

type OpenPane struct {
	Entrypoint   string
	Placement    string // overlay | popup | split | tab | zoomed
	Direction    string // right | down (split only)
	TargetPaneID string
	Width        any // int cells or "80%" (popup only)
	Height       any
	Env          map[string]string
	Focus        bool
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("herdr %s: %s", e.Code, e.Message) }

// IsNotFound reports a missing object. Herdr uses specific codes such as
// "pane_not_found" (0.8.2), so any code ending in "_not_found" counts.
func IsNotFound(err error) bool {
	var he *Error
	return errors.As(err, &he) && (he.Code == "not_found" || strings.HasSuffix(he.Code, "_not_found"))
}

// InvocationContext is the subset of HERDR_PLUGIN_CONTEXT_JSON herdr-dots reads.
type InvocationContext struct {
	FocusedPaneID    string `json:"focused_pane_id"`
	FocusedPaneAgent string `json:"focused_pane_agent"`
	FocusedPaneCwd   string `json:"focused_pane_cwd"`
	TabID            string `json:"tab_id"`
	WorkspaceID      string `json:"workspace_id"`
}

func ParseInvocationContext(raw string) (InvocationContext, error) {
	var ic InvocationContext
	if raw == "" {
		return ic, nil
	}
	err := json.Unmarshal([]byte(raw), &ic)
	return ic, err
}
