package engine

import (
	"testing"

	"herdr-dots/internal/backend"
)

// Final review item 8: a run that never reached the provider costs a known $0;
// a real run with tokens and no price stays unknown.
func TestDeltaOfNoTokensIsAKnownZero(t *testing.T) {
	if d := deltaOf(backend.Result{}); !d.CostKnown || d.CostUSD != 0 || d.Calls != 1 {
		t.Fatalf("no tokens, no cost: %+v", d)
	}
	if d := deltaOf(backend.Result{InputTokens: 120, OutputTokens: 30}); d.CostKnown {
		t.Fatalf("an unpriced run with tokens must stay unknown: %+v", d)
	}
	if d := deltaOf(backend.Result{InputTokens: 120, CostUSD: 0.2, CostKnown: true}); !d.CostKnown || d.CostUSD != 0.2 {
		t.Fatalf("priced run: %+v", d)
	}
}
