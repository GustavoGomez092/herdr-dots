package toolloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"herdr-dots/internal/pricing"
	"herdr-dots/internal/textutil"
)

// Call is one tool call a model asked for.
type Call struct {
	ID   string
	Name string
	Args json.RawMessage
}

// Result answers one Call. Tool failures are results with IsError, never run
// failures: the model sees the error and carries on.
type Result struct {
	CallID  string
	Name    string
	Content string
	IsError bool
}

// Reply is one provider turn mapped to neutral types.
type Reply struct {
	Text      string
	Calls     []Call
	Usage     pricing.Usage
	CostUSD   float64
	CostKnown bool
	Model     string // the model that produced this turn
}

// TurnFunc sends one request. results answer the previous turn's calls (nil on
// the first turn). final means: no more tool calls; set tool_choice none (or
// drop the tools) and include FinalNudge after the results.
type TurnFunc func(ctx context.Context, results []Result, final bool) (Reply, error)

// Outcome is a finished loop.
type Outcome struct {
	Text      string
	Usage     pricing.Usage
	CostUSD   float64
	CostKnown bool // every turn's cost was known
	Model     string
	Calls     int  // tool calls executed
	Capped    bool // the call cap was reached
	Turns     int
}

// Driver runs the tool loop.
type Driver struct {
	Tools  []Tool
	Limits Limits
	Logf   func(format string, args ...any) // jail violations; nil = silent
}

// Run drives turn until the model answers without tool calls. After
// Limits.MaxCalls tool calls it sends one final turn with tools disabled.
// The Outcome is filled even on error, so spend is always recorded.
func (d Driver) Run(ctx context.Context, turn TurnFunc) (Outcome, error) {
	out := Outcome{CostKnown: true}
	maxCalls := d.Limits.MaxCalls
	if maxCalls <= 0 {
		maxCalls = DefaultLimits().MaxCalls
	}
	var results []Result
	final := len(d.Tools) == 0
	for {
		reply, err := turn(ctx, results, final)
		out.Turns++
		out.Usage = out.Usage.Add(reply.Usage)
		out.CostUSD += reply.CostUSD
		out.CostKnown = out.CostKnown && reply.CostKnown
		if reply.Model != "" {
			out.Model = reply.Model
		}
		if err != nil {
			return out, err
		}
		if len(reply.Calls) == 0 || final {
			out.Text = reply.Text
			return out, nil
		}
		results = results[:0:0]
		for _, c := range reply.Calls {
			if out.Calls >= maxCalls {
				out.Capped = true
				results = append(results, Result{CallID: c.ID, Name: c.Name, Content: "tool budget exhausted", IsError: true})
				continue
			}
			out.Calls++
			results = append(results, d.execute(ctx, c))
		}
		if out.Calls >= maxCalls {
			out.Capped, final = true, true
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
	}
}

func (d Driver) execute(ctx context.Context, c Call) Result {
	res := Result{CallID: c.ID, Name: c.Name}
	var tool *Tool
	for i := range d.Tools {
		if d.Tools[i].Name == c.Name {
			tool = &d.Tools[i]
		}
	}
	if tool == nil {
		res.Content, res.IsError = fmt.Sprintf("unknown tool %q", c.Name), true
		return res
	}
	args := c.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	text, err := tool.Run(ctx, args)
	if err != nil {
		if (errors.Is(err, ErrOutsideRoot) || errors.Is(err, ErrDenied)) && d.Logf != nil {
			// The name and args are model text: strip control characters
			// before they reach the engine log (reconciled, Phase 2 rule).
			line := fmt.Sprintf("tool %s refused: %v (args %s)", c.Name, err, clip(string(args), 200))
			d.Logf("%s", textutil.SanitizeModelText(line))
		}
		res.Content, res.IsError = textutil.SanitizeModelText(err.Error()), true
		return res
	}
	limit := d.Limits.MaxResultBytes
	if limit <= 0 {
		limit = DefaultLimits().MaxResultBytes
	}
	// Repo files are untrusted: strip terminal control sequences before the
	// text goes back to the model (and on to logs), then cap it.
	res.Content = capBytes(textutil.SanitizeModelText(text), limit)
	return res
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
