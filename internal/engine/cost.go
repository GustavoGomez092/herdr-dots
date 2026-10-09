package engine

import (
	"errors"
	"fmt"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/pricing"
	"herdr-dots/internal/state"
)

// OverUSD reports whether a dollar budget is reached. budget <= 0 is off.
func OverUSD(cost, budget float64) bool { return budget > 0 && cost >= budget }

// requestFor builds a review request from the dot (memory fields are set by
// the caller, as Phase 2 does).
func requestFor(d dots.Dot, cwd string) backend.Request {
	return backend.Request{
		DotName: d.Name, Purpose: d.Purpose, Verbosity: d.Context.Verbosity,
		Model: d.Backend.Model, Args: d.Backend.Args, RepoRead: d.Context.RepoRead, Cwd: cwd,
		Effort: d.Backend.Effort, MaxTokens: d.Backend.MaxTokens, BaseURL: d.Backend.BaseURL,
		ContextWindow: d.Backend.ContextWindow, Fallbacks: d.Backend.Fallbacks,
	}
}

// deltaOf is the usage a run adds to state.Usage.
func deltaOf(res backend.Result) state.Delta {
	return state.Delta{Calls: 1, In: res.InputTokens, Out: res.OutputTokens,
		CacheRead: res.CacheReadTokens, CacheWrite: res.CacheWriteTokens,
		CostUSD: res.CostUSD, CostKnown: res.CostKnown}
}

// costDetail is the feed suffix for a finished run:
// "cost=$0.0123 tokens=1234/210 tools=3 model=claude-opus-4-8".
func costDetail(d dots.Dot, res backend.Result) string {
	s := "cost=?"
	if res.CostKnown {
		s = fmt.Sprintf("cost=$%.4f", res.CostUSD)
	}
	s += fmt.Sprintf(" tokens=%d/%d", res.InputTokens, res.OutputTokens)
	if res.ToolCalls > 0 {
		s += fmt.Sprintf(" tools=%d", res.ToolCalls)
		if res.ToolsCapped {
			s += " (cap)"
		}
	}
	// The model that answered, when it isn't the one asked for: a refusal
	// fallback. A dated snapshot of the asked model ("gpt-6-sol-2026-08-01")
	// is the same model; a dot with no model asked for the kind's default.
	want := d.Backend.Model
	if want == "" {
		want = dots.DefaultModelFor(d.Backend.Kind)
	}
	if res.Model != "" && want != "" && !pricing.SameModel(res.Model, want) {
		s += " model=" + res.Model
	}
	return s
}

// redactErr scrubs key-shaped text from a run error before it reaches the
// engine log, the feed, a council card or a notification (adapters redact
// already; this is defense in depth). An error with nothing to scrub is
// returned as is, so errors.Is still sees context.DeadlineExceeded.
func redactErr(err error) error {
	if err == nil {
		return nil
	}
	if s := credentials.Redact(err.Error()); s != err.Error() {
		return errors.New(s)
	}
	return err
}

// withCost appends the cost detail to a flagged run's message.
func withCost(msg, detail string) string {
	if msg == "" {
		return detail
	}
	return msg + " · " + detail
}
