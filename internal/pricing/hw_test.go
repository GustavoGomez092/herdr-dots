package pricing

import "testing"

// C2 (post-rc hardening, test gap): the date-strip step on its own. The
// served id is a dated snapshot the table doesn't list, and the requested
// id is another priced model: the run is priced at the undated served
// model, never at the requested one or as unknown.
func TestCostServedPricesTheUndatedServedModelBeforeTheRequestedOne(t *testing.T) {
	tab := Default()
	u := Usage{In: 1000, Out: 100}
	haiku, ok := tab.Cost("anthropic", "claude-haiku-4-5", u)
	opus, _ := tab.Cost("anthropic", "claude-opus-5-5", u)
	if !ok || near(haiku, opus) {
		t.Fatalf("the test needs two different prices: %v %v", haiku, opus)
	}
	if _, listed := tab.Lookup("anthropic", "claude-haiku-4-5-20251001"); listed {
		t.Fatal("the dated id must not be in the table for this test")
	}
	usd, ok := tab.CostServed("anthropic", "claude-haiku-4-5-20251001", "claude-opus-5-5", u)
	if !ok || !near(usd, haiku) {
		t.Fatalf("CostServed = %v %v, want the haiku price %v", usd, ok, haiku)
	}
}
