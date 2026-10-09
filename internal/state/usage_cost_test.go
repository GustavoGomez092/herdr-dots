package state

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestAddTracksDollarsAndPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	u, err := OpenUsage(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = u.Add("w1:p1", "sec", Delta{Calls: 1, In: 100, Out: 10, CacheRead: 50, CacheWrite: 5, CostUSD: 0.25, CostKnown: true})
	_ = u.Add("w1:p1", "sec", Delta{Calls: 1, In: 10, Out: 1, CostUSD: 0.5, CostKnown: true})
	_ = u.Add("w1:p1", "perf", Delta{Calls: 1, In: 10, CostUSD: 99, CostKnown: false}) // unknown price
	u2, _ := OpenUsage(p)
	if got := u2.Cost("w1:p1", "sec"); got != 0.75 {
		t.Fatalf("Cost = %v", got)
	}
	if got := u2.PaneCost("w1:p1"); got != 0.75 {
		t.Fatalf("PaneCost = %v: unknown costs never count", got)
	}
	if u2.Calls("w1:p1", "perf") != 1 || u2.CostKnown("w1:p1", "perf") || !u2.CostKnown("w1:p1", "sec") {
		t.Fatal("unknown-cost calls still count as calls and mark the dot unknown")
	}
	if u2.PaneCalls("w1:p1") != 3 {
		t.Fatal("PaneCalls")
	}
}

func TestAddNeverStoresNaNOrNegativeCost(t *testing.T) {
	u, _ := OpenUsage(filepath.Join(t.TempDir(), "usage.json"))
	_ = u.Add("p", "d", Delta{Calls: 1, CostUSD: math.NaN(), CostKnown: true})
	_ = u.Add("p", "d", Delta{Calls: 1, CostUSD: math.Inf(1), CostKnown: true})
	_ = u.Add("p", "d", Delta{Calls: 1, CostUSD: -2, CostKnown: true})
	if c := u.PaneCost("p"); c != 0 || math.IsNaN(c) {
		t.Fatalf("PaneCost = %v", c)
	}
}

func TestOldUsageFileLoadsWithZeroCost(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(p, []byte(`{"w1:p1":{"sec":{"calls":4,"input_tokens":10,"output_tokens":2}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := OpenUsage(p)
	if err != nil || u.Calls("w1:p1", "sec") != 4 || u.Cost("w1:p1", "sec") != 0 || !u.CostKnown("w1:p1", "sec") {
		t.Fatalf("%v", err)
	}
	if err := u.Record("w1:p1", "sec", 1, 1); err != nil || u.Calls("w1:p1", "sec") != 5 {
		t.Fatal("Record still counts one call")
	}
}

func TestZeroCallDeltaWithUnknownCostIsFlagged(t *testing.T) {
	u, _ := OpenUsage(filepath.Join(t.TempDir(), "usage.json"))
	if !u.PaneCostKnown("p") {
		t.Fatal("an empty pane has no unpriced runs")
	}
	_ = u.Add("p", "d", Delta{In: 5})
	if u.CostKnown("p", "d") || u.PaneCostKnown("p") {
		t.Fatal("a delta with no price must be flagged unknown even with Calls=0")
	}
	if u.Cost("p", "d") != 0 {
		t.Fatal("unknown cost never counts")
	}
}

func TestPaneCostKnownSeesAnyDot(t *testing.T) {
	u, _ := OpenUsage(filepath.Join(t.TempDir(), "usage.json"))
	_ = u.Add("p", "a", Delta{Calls: 1, CostUSD: 1, CostKnown: true})
	if !u.PaneCostKnown("p") {
		t.Fatal("all priced")
	}
	_ = u.Add("p", "council", Delta{Calls: 1})
	if u.PaneCostKnown("p") || !u.PaneCostKnown("other") {
		t.Fatal("an unpriced run on any slug, the council's included, taints only its pane")
	}
}

func TestResetClearsDollarsToo(t *testing.T) {
	u, _ := OpenUsage(filepath.Join(t.TempDir(), "usage.json"))
	_ = u.Add("p", "d", Delta{Calls: 1, CostUSD: 1, CostKnown: true})
	_ = u.ResetDot("p", "d")
	if u.PaneCost("p") != 0 {
		t.Fatal("ResetDot")
	}
	_ = u.Add("p", "d", Delta{Calls: 1, CostUSD: 1, CostKnown: true})
	_ = u.ResetPane("p")
	if u.Cost("p", "d") != 0 {
		t.Fatal("ResetPane")
	}
}
