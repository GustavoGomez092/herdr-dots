// Package engine runs dots: it watches main agent panes, decides which dots
// should look at each finished turn, runs them, and routes their verdicts.
package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

type Reason string

const (
	ReasonRun           Reason = "run"
	ReasonPausedAll     Reason = "paused_all"
	ReasonPaused        Reason = "paused"
	ReasonObserve       Reason = "observe"
	ReasonOff           Reason = "on_turn_off"
	ReasonFromDot       Reason = "from_dot"
	ReasonCooldown      Reason = "cooldown"
	ReasonGlobs         Reason = "globs"
	ReasonBudget        Reason = "budget"
	ReasonSessionBudget Reason = "session_budget"
	ReasonConsulted     Reason = "consulted" // asked through MCP during these turns (Phase 5)
)

type FilterInput struct {
	Consulted bool // the dot was asked through the MCP server during these turns: skip them once

	Dot        dots.Dot
	Att        state.DotAttachment
	NewTurns   []transcript.Turn
	PausedAll  bool
	DotCalls   int
	PaneCalls  int
	PaneBudget int

	// Dollar budgets (Phase 4); a budget <= 0 is off. Costs are known
	// costs only: runs without a price never count.
	DotCostUSD    float64
	PaneCostUSD   float64
	DotBudgetUSD  float64
	PaneBudgetUSD float64
}

type Decision struct {
	Run    bool
	Reason Reason
	Turns  []transcript.Turn // new turns minus the ones this dot started
}

// Decide applies the spec's trigger filter (§6.3) to one dot.
func Decide(in FilterInput) Decision {
	switch {
	case in.PausedAll:
		return Decision{Reason: ReasonPausedAll}
	case in.Att.Paused:
		return Decision{Reason: ReasonPaused}
	case in.Dot.Delivery.Mode == dots.ModeObserve:
		return Decision{Reason: ReasonObserve}
	case !in.Dot.Trigger.OnTurn:
		return Decision{Reason: ReasonOff}
	case in.Consulted:
		return Decision{Reason: ReasonConsulted}
	}
	var turns []transcript.Turn
	for _, t := range in.NewTurns {
		if !t.IsFromDot(in.Dot.Name) {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		return Decision{Reason: ReasonFromDot}
	}
	if in.Att.CooldownLeft > 0 {
		return Decision{Reason: ReasonCooldown}
	}
	if len(in.Dot.Trigger.FileGlobs) > 0 && !globsMatch(in.Dot.Trigger.FileGlobs, turns) {
		return Decision{Reason: ReasonGlobs}
	}
	if in.DotCalls >= in.Dot.Budget.MaxCallsPerSession || OverUSD(in.DotCostUSD, in.DotBudgetUSD) {
		return Decision{Reason: ReasonBudget}
	}
	if in.PaneCalls >= in.PaneBudget || OverUSD(in.PaneCostUSD, in.PaneBudgetUSD) {
		return Decision{Reason: ReasonSessionBudget}
	}
	return Decision{Run: true, Reason: ReasonRun, Turns: turns}
}

// budgetMessage is the toast for a dot stopped by a budget: which budget
// (the dot's or the pane's, calls or dollars) and how it resets. r resets
// only the dot's own counters, so a pane budget doesn't suggest it.
func budgetMessage(in FilterInput, reason Reason) string {
	if reason == ReasonSessionBudget {
		const resets = "; resets on a new agent session"
		if in.PaneCalls < in.PaneBudget && OverUSD(in.PaneCostUSD, in.PaneBudgetUSD) {
			return fmt.Sprintf("pane dollar budget reached ($%.2f of $%.2f)%s", in.PaneCostUSD, in.PaneBudgetUSD, resets)
		}
		return fmt.Sprintf("pane call budget reached (%s)%s", calls(in.PaneBudget), resets)
	}
	const resets = "; resets on a new agent session or press r"
	if in.DotCalls < in.Dot.Budget.MaxCallsPerSession && OverUSD(in.DotCostUSD, in.DotBudgetUSD) {
		return fmt.Sprintf("dot dollar budget reached ($%.2f of $%.2f)%s", in.DotCostUSD, in.DotBudgetUSD, resets)
	}
	return fmt.Sprintf("dot call budget reached (%s)%s", calls(in.Dot.Budget.MaxCallsPerSession), resets)
}

func calls(n int) string {
	if n == 1 {
		return "1 call"
	}
	return fmt.Sprintf("%d calls", n)
}

// globsMatch tries each touched path as-is, without its leading slash, and by
// base name, so "**/*.tsx" and "*.tsx" both match "/repo/web/app.tsx".
func globsMatch(globs []string, turns []transcript.Turn) bool {
	for _, t := range turns {
		if pathsMatchGlobs(globs, t.TouchedPaths()) {
			return true
		}
	}
	return false
}

// pathsMatchGlobs is globsMatch for a plain path list (a commit's files).
func pathsMatchGlobs(globs, paths []string) bool {
	for _, p := range paths {
		for _, g := range globs {
			for _, cand := range []string{p, strings.TrimPrefix(p, "/"), filepath.Base(p)} {
				if ok, _ := doublestar.Match(g, cand); ok {
					return true
				}
			}
		}
	}
	return false
}
