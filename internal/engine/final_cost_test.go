package engine

import (
	"strings"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
)

// Final review item 1: a dated snapshot of the requested model is the same
// model, so the feed line carries no model= suffix.
func TestCostDetailTreatsADatedSnapshotAsTheSameModel(t *testing.T) {
	d := apiDot("Perf")
	d.Backend.Kind, d.Backend.Model = dots.KindOpenAIAPI, "gpt-6-sol"
	res := backend.Result{CostKnown: true, CostUSD: 0.01, InputTokens: 10, OutputTokens: 2, Model: "gpt-6-sol-2026-08-01"}
	if s := costDetail(d, res); strings.Contains(s, "model=") {
		t.Fatalf("dated snapshot shown as a different model: %q", s)
	}
	res.Model = "gpt-6-astra"
	if s := costDetail(d, res); !strings.Contains(s, "model=gpt-6-astra") {
		t.Fatalf("a different model must show: %q", s)
	}
}

// Final review item 9: an anthropic-api dot with no model compares the served
// model against the kind's default, so a refusal fallback shows.
func TestCostDetailEmptyAnthropicModelUsesTheDefault(t *testing.T) {
	d := apiDot("Security")
	d.Backend.Model = ""
	res := backend.Result{CostKnown: true, Model: "claude-sonnet-5-5"}
	if s := costDetail(d, res); !strings.Contains(s, "model=claude-sonnet-5-5") {
		t.Fatalf("fallback model hidden: %q", s)
	}
	res.Model = dots.DefaultAnthropicModel
	if s := costDetail(d, res); strings.Contains(s, "model=") {
		t.Fatalf("the default model is not a fallback: %q", s)
	}
}
