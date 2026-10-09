package state

import "sync"

type Counter struct {
	Calls        int `json:"calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Usage counts backend calls per main pane and dot. Budgets are per pane.
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

func (u *Usage) Record(paneID, slug string, in, out int) error {
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
	c.Calls++
	c.InputTokens += in
	c.OutputTokens += out
	return writeJSON(u.path, u.data)
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
