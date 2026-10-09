package dots

import (
	"fmt"
	"slices"
	"strings"
)

// Capabilities a dot written by someone else (an imported pack, a project
// dot) may ask for. Each starts clamped and is restored only when the
// developer keeps it, one by one.
const (
	CapInject   = "inject"   // delivery.mode = "inject": text reaches the agent unapproved
	CapWrite    = "write"    // capabilities.write (Phase 7): drafts edits in a worktree
	CapMCP      = "mcp"      // capabilities.mcp: the agent can ask the dot over MCP, unapproved
	CapResearch = "research" // research.enabled (Phase 8): runs while you're away
	CapWeb      = "web"      // research.web (Phase 8): fetches from the internet
	CapSchedule = "schedule" // schedule.cron (Phase 8): runs on a timer
	CapCommit   = "commit"   // trigger.on_commit (Phase 8): runs on every commit
	CapBaseURL  = "base_url" // backend.base_url (Phase 4): sends prompts (and an API key) to another endpoint
)

// CapArgs is the clamp that removes denied backend args; CapProjects removes
// absolute schedule.projects paths (Phase 8), which name directories on the
// author's machine: someone else's dot may run only on the projects it is
// attached to. Neither can be kept.
const (
	CapArgs     = "args"
	CapProjects = "projects"
)

// Capabilities lists the keepable capabilities in display order.
var Capabilities = []string{CapInject, CapWrite, CapMCP, CapResearch, CapWeb, CapSchedule, CapCommit, CapBaseURL}

// Clamp is one change ClampDot made.
type Clamp struct {
	Capability string // one of Capabilities, or CapArgs
	Field      string // the TOML key, e.g. "delivery.mode"
	From, To   string // display values
	Keepable   bool
}

func (c Clamp) String() string {
	to := c.To
	if to == "" {
		to = "(off)"
	}
	return fmt.Sprintf("%s: %s %s → %s", c.Capability, c.Field, c.From, to)
}

// CheckCapabilities refuses names that aren't keepable capabilities.
func CheckCapabilities(caps []string) error {
	for _, c := range caps {
		if !slices.Contains(Capabilities, c) {
			return fmt.Errorf("unknown capability %q: want one of %s", c, strings.Join(Capabilities, ", "))
		}
	}
	return nil
}

// ClampDot returns d with every risky capability turned off except those in
// keep, and the clamps it applied. Denied backend args are always removed.
// d itself is not changed.
//
// It fails closed: unless write is kept, [capabilities] is rebuilt from the
// fields readOnly knows, so a capability field added later starts off.
// TestClampDotHandlesEveryDotField fails until every new Dot field is either
// clamped here or reviewed as safe.
func ClampDot(d Dot, keep []string) (Dot, []Clamp) {
	d.Backend.Args = slices.Clone(d.Backend.Args)
	d.Schedule.Projects = slices.Clone(d.Schedule.Projects)
	var out []Clamp
	kept := func(c string) bool { return slices.Contains(keep, c) }
	clamp := func(c, field, from, to string) {
		out = append(out, Clamp{Capability: c, Field: field, From: from, To: to, Keepable: true})
	}
	if d.Delivery.Mode == ModeInject && !kept(CapInject) {
		d.Delivery.Mode = ModeQueue
		clamp(CapInject, "delivery.mode", string(ModeInject), string(ModeQueue))
	}
	if !kept(CapWrite) {
		if d.Capabilities.Write {
			clamp(CapWrite, "capabilities.write", "true", "false")
		}
		d.Capabilities = readOnly(d.Capabilities)
	}
	if d.Capabilities.MCP && !kept(CapMCP) {
		d.Capabilities.MCP = false
		clamp(CapMCP, "capabilities.mcp", "true", "false")
	}
	if d.Research.Enabled && !kept(CapResearch) {
		d.Research.Enabled = false
		clamp(CapResearch, "research.enabled", "true", "false")
	}
	if d.Research.Web && !kept(CapWeb) {
		d.Research.Web = false
		clamp(CapWeb, "research.web", "true", "false")
	}
	if d.Schedule.Cron != "" && !kept(CapSchedule) {
		clamp(CapSchedule, "schedule.cron", fmt.Sprintf("%q", d.Schedule.Cron), "")
		d.Schedule.Cron = ""
	}
	if d.Trigger.OnCommit && !kept(CapCommit) {
		d.Trigger.OnCommit = false
		clamp(CapCommit, "trigger.on_commit", "true", "false")
	}
	if d.Backend.BaseURL != "" && !kept(CapBaseURL) {
		clamp(CapBaseURL, "backend.base_url", fmt.Sprintf("%q", d.Backend.BaseURL), "")
		d.Backend.BaseURL = ""
	}
	if denied := DeniedArgs(d.Backend.Kind, d.Backend.Args); len(denied) > 0 {
		out = append(out, Clamp{Capability: CapArgs, Field: "backend.args", From: strings.Join(denied, " "), To: "(removed)"})
		d.Backend.Args = SafeArgs(d.Backend.Kind, d.Backend.Args)
		if d.Backend.Args == nil {
			d.Backend.Args = []string{}
		}
	}
	attached := func(p string) bool { return p == ScheduleAttached }
	if abs := slices.DeleteFunc(slices.Clone(d.Schedule.Projects), attached); len(abs) > 0 {
		out = append(out, Clamp{Capability: CapProjects, Field: "schedule.projects", From: strings.Join(abs, ", "), To: "(removed)"})
		d.Schedule.Projects = slices.DeleteFunc(d.Schedule.Projects, func(p string) bool { return !attached(p) })
	}
	return d, out
}

// readOnly is c with Write off. It rebuilds the struct instead of assigning
// to .Write: Phase 7's write-tool guard (TestOnlyDraftsAskForWriteTools in
// internal/engine) refuses any assignment to a .Write field outside
// internal/engine/draft.go. A field it doesn't list is dropped (off):
// TestClampKeepsEveryOtherCapabilityField fails if a field added later is
// dropped here by mistake.
func readOnly(c CapabilitySpec) CapabilitySpec {
	return CapabilitySpec{WriteTimeoutSeconds: c.WriteTimeoutSeconds, MaxPatchBytes: c.MaxPatchBytes, MaxFiles: c.MaxFiles, MCP: c.MCP}
}

// Requested lists the keepable capabilities d asks for, in display order.
func Requested(d Dot) []string {
	_, cs := ClampDot(d, nil)
	var out []string
	for _, c := range cs {
		if c.Keepable {
			out = append(out, c.Capability)
		}
	}
	return out
}
