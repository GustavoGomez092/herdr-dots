package state

import (
	"math"
	"sync"
)

type Counter struct {
	Calls        int `json:"calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`

	// Phase 4. Old usage.json files load with zeros.
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`     // known costs only
	UnknownCostCalls int     `json:"unknown_cost,omitempty"` // runs whose cost is unknown
}

// Delta is one run's usage. CostUSD counts only when CostKnown.
type Delta struct {
	Calls, In, Out, CacheRead, CacheWrite int
	CostUSD                               float64
	CostKnown                             bool
}

// Usage counts backend calls, tokens and dollars per main pane and dot.
// Budgets are per pane.
type Usage struct {
	path string
	mu   sync.Mutex
	data map[string]map[string]*Counter
}

func OpenUsage(path string) (*Usage, error) {
	u := &Usage{path: path, data: map[string]map[string]*Counter{}}
	if err := readJSON(path, &u.data); err != nil {
		return nil, err
	}
	if u.data == nil {
		u.data = map[string]map[string]*Counter{}
	}
	return u, nil
}

// Record is v1's call counter: Add with one call and no cost.
func (u *Usage) Record(paneID, slug string, in, out int) error {
	return u.Add(paneID, slug, Delta{Calls: 1, In: in, Out: out})
}

// Add records one run. A cost that is unknown, negative, NaN or infinite is
// never added (it counts as an unknown-cost call), so dollar totals stay
// finite and dollar budgets ignore it.
func (u *Usage) Add(paneID, slug string, d Delta) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.data[paneID] == nil {
		u.data[paneID] = map[string]*Counter{}
	}
	c := u.data[paneID][slug]
	if c == nil {
		c = &Counter{}
		u.data[paneID][slug] = c
	}
	c.Calls += d.Calls
	c.InputTokens += d.In
	c.OutputTokens += d.Out
	c.CacheReadTokens += d.CacheRead
	c.CacheWriteTokens += d.CacheWrite
	if d.CostKnown && d.CostUSD >= 0 && !math.IsNaN(d.CostUSD) && !math.IsInf(d.CostUSD, 0) {
		c.CostUSD += d.CostUSD
	} else {
		// Unknown cost is always visible, even for a delta with Calls == 0.
		c.UnknownCostCalls += max(d.Calls, 1)
	}
	return writeJSON(u.path, u.data)
}

// Cost is one dot's known spend on a pane, in USD.
func (u *Usage) Cost(paneID, slug string) float64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	if c := u.data[paneID][slug]; c != nil {
		return c.CostUSD
	}
	return 0
}

// CostKnown is false once any of the dot's runs on the pane had no price.
func (u *Usage) CostKnown(paneID, slug string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if c := u.data[paneID][slug]; c != nil {
		return c.UnknownCostCalls == 0
	}
	return true
}

// PaneCostKnown is false once any run on the pane, by any dot or the council
// chair, had no price.
func (u *Usage) PaneCostKnown(paneID string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, c := range u.data[paneID] {
		if c.UnknownCostCalls > 0 {
			return false
		}
	}
	return true
}

// PaneCost is the pane's known spend across dots, in USD.
func (u *Usage) PaneCost(paneID string) float64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	t := 0.0
	for _, c := range u.data[paneID] {
		t += c.CostUSD
	}
	return t
}

func (u *Usage) Calls(paneID, slug string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	if c := u.data[paneID][slug]; c != nil {
		return c.Calls
	}
	return 0
}

func (u *Usage) PaneCalls(paneID string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, c := range u.data[paneID] {
		n += c.Calls
	}
	return n
}

// ResetDot clears one dot's counter on a pane (the pane total drops with it).
func (u *Usage) ResetDot(paneID, slug string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.data[paneID][slug]; !ok {
		return nil
	}
	delete(u.data[paneID], slug)
	return writeJSON(u.path, u.data)
}

func (u *Usage) ResetPane(paneID string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.data, paneID)
	return writeJSON(u.path, u.data)
}
