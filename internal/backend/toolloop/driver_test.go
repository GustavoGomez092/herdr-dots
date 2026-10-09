package toolloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"herdr-dots/internal/pricing"
)

// script answers each turn from a list and records what the driver sent.
type script struct {
	replies []Reply
	sent    [][]Result
	finals  []bool
}

func (s *script) turn(_ context.Context, results []Result, final bool) (Reply, error) {
	s.sent = append(s.sent, append([]Result(nil), results...))
	s.finals = append(s.finals, final)
	i := len(s.sent) - 1
	if i >= len(s.replies) {
		return Reply{}, fmt.Errorf("unexpected turn %d", i)
	}
	return s.replies[i], nil
}

func echoTool(name string) Tool {
	return Tool{Name: name, Run: func(_ context.Context, a json.RawMessage) (string, error) { return "ok " + string(a), nil }}
}

func callN(n int) []Call {
	var cs []Call
	for i := 0; i < n; i++ {
		cs = append(cs, Call{ID: fmt.Sprintf("c%d", i), Name: "echo", Args: json.RawMessage(`{}`)})
	}
	return cs
}

func TestDriverRoundTripSumsUsageAndCost(t *testing.T) {
	s := &script{replies: []Reply{
		{Calls: callN(2), Usage: pricing.Usage{In: 10, Out: 2}, CostUSD: 0.01, CostKnown: true, Model: "m1"},
		{Text: `{"relevant":false}`, Usage: pricing.Usage{In: 20, Out: 3, CacheRead: 5}, CostUSD: 0.02, CostKnown: true, Model: "m2"},
	}}
	out, err := Driver{Tools: []Tool{echoTool("echo")}, Limits: DefaultLimits()}.Run(context.Background(), s.turn)
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != `{"relevant":false}` || out.Calls != 2 || out.Capped || out.Turns != 2 || out.Model != "m2" {
		t.Fatalf("outcome %+v", out)
	}
	if out.Usage != (pricing.Usage{In: 30, Out: 5, CacheRead: 5}) || out.CostUSD != 0.03 || !out.CostKnown {
		t.Fatalf("usage %+v cost %v %v", out.Usage, out.CostUSD, out.CostKnown)
	}
	if len(s.sent[1]) != 2 || s.sent[1][0].CallID != "c0" || !strings.HasPrefix(s.sent[1][0].Content, "ok") || s.finals[1] {
		t.Fatalf("results sent %+v finals %v", s.sent[1], s.finals)
	}
}

func TestDriverStopsAtCapWithOneFinalTurn(t *testing.T) {
	s := &script{replies: []Reply{
		{Calls: callN(5)},
		{Calls: callN(5)}, // 3 run, 2 over the cap
		{Text: "final answer"},
	}}
	lim := DefaultLimits() // MaxCalls 8
	out, err := Driver{Tools: []Tool{echoTool("echo")}, Limits: lim}.Run(context.Background(), s.turn)
	if err != nil || out.Text != "final answer" || out.Calls != 8 || !out.Capped {
		t.Fatalf("%+v %v", out, err)
	}
	if s.finals[0] || s.finals[1] || !s.finals[2] {
		t.Fatalf("finals = %v, want only the last turn final", s.finals)
	}
	over := s.sent[2]
	if len(over) != 5 || !over[4].IsError || over[4].Content != "tool budget exhausted" || over[2].IsError {
		t.Fatalf("over-cap results %+v", over)
	}
}

func TestDriverTreatsToolCallsOnTheFinalTurnAsTheAnswer(t *testing.T) {
	s := &script{replies: []Reply{{Text: "x", Calls: callN(1)}}}
	out, err := Driver{}.Run(context.Background(), s.turn) // no tools: first turn is final
	if err != nil || out.Text != "x" || !s.finals[0] {
		t.Fatalf("%+v %v %v", out, err, s.finals)
	}
}

func TestToolErrorsGoBackToTheModel(t *testing.T) {
	var logged []string
	bad := Tool{Name: "read_file", Run: func(context.Context, json.RawMessage) (string, error) { return "", ErrOutsideRoot }}
	s := &script{replies: []Reply{
		{Calls: []Call{{ID: "a", Name: "read_file", Args: json.RawMessage(`{"path":"../../.ssh/id_rsa"}`)}, {ID: "b", Name: "nope"}}},
		{Text: "done"},
	}}
	d := Driver{Tools: []Tool{bad}, Logf: func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }}
	out, err := d.Run(context.Background(), s.turn)
	if err != nil || out.Text != "done" {
		t.Fatalf("%+v %v", out, err)
	}
	r := s.sent[1]
	if !r[0].IsError || r[0].Content != "path outside project" || !r[1].IsError || !strings.Contains(r[1].Content, "unknown tool") {
		t.Fatalf("results %+v", r)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "path outside project") {
		t.Fatalf("jail violation must be logged: %v", logged)
	}
}

// The refusal log line carries model text (tool name and args): it is
// sanitized like every other piece of model text (reconciled).
func TestRefusalLogIsSanitized(t *testing.T) {
	var logged []string
	bad := Tool{Name: "read_file", Run: func(context.Context, json.RawMessage) (string, error) { return "", ErrDenied }}
	s := &script{replies: []Reply{
		{Calls: []Call{{ID: "a", Name: "read_file", Args: json.RawMessage("{\"path\":\".env\u009b2J\x7f\"}")}}},
		{Text: "done"},
	}}
	d := Driver{Tools: []Tool{bad}, Logf: func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }}
	if _, err := d.Run(context.Background(), s.turn); err != nil {
		t.Fatal(err)
	}
	if len(logged) != 1 || strings.ContainsAny(logged[0], "\u009b\x7f") || !strings.Contains(logged[0], ".env") {
		t.Fatalf("log line must be sanitized: %q", logged)
	}
}

// Tool output comes from repo files: control sequences must not reach the model.
func TestToolResultsAreSanitized(t *testing.T) {
	evil := Tool{Name: "echo", Run: func(context.Context, json.RawMessage) (string, error) {
		return "line\x1b]0;pwned\x07 ok\x1b[2J\u009b31m end", nil
	}}
	s := &script{replies: []Reply{{Calls: callN(1)}, {Text: "done"}}}
	if _, err := (Driver{Tools: []Tool{evil}}).Run(context.Background(), s.turn); err != nil {
		t.Fatal(err)
	}
	got := s.sent[1][0].Content
	if strings.ContainsAny(got, "\x1b\x07\u009b") || !strings.Contains(got, "line") || !strings.Contains(got, "end") {
		t.Fatalf("result not sanitized: %q", got)
	}
}

func TestToolResultsAreCappedAt64KB(t *testing.T) {
	big := Tool{Name: "echo", Run: func(context.Context, json.RawMessage) (string, error) { return strings.Repeat("a", 200<<10), nil }}
	s := &script{replies: []Reply{{Calls: callN(1)}, {Text: "done"}}}
	if _, err := (Driver{Tools: []Tool{big}}).Run(context.Background(), s.turn); err != nil {
		t.Fatal(err)
	}
	if n := len(s.sent[1][0].Content); n > 64<<10 {
		t.Fatalf("result %d bytes", n)
	}
}

func TestDriverKeepsSpendOnError(t *testing.T) {
	boom := errors.New("refused (cyber)")
	turn := func(context.Context, []Result, bool) (Reply, error) {
		return Reply{Usage: pricing.Usage{In: 7}, CostUSD: 0.5, CostKnown: true}, boom
	}
	out, err := Driver{}.Run(context.Background(), turn)
	if !errors.Is(err, boom) || out.Usage.In != 7 || out.CostUSD != 0.5 {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestUnknownCostTurnMakesTheRunUnknown(t *testing.T) {
	s := &script{replies: []Reply{{Calls: callN(1), CostKnown: true}, {Text: "t", CostKnown: false}}}
	out, _ := Driver{Tools: []Tool{echoTool("echo")}}.Run(context.Background(), s.turn)
	if out.CostKnown {
		t.Fatal("one unknown turn makes the run's cost unknown")
	}
}
