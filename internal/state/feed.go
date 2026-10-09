package state

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"

	"herdr-dots/internal/textutil"
)

const (
	OutcomeFlagged = "flagged"
	OutcomePass    = "pass"
	OutcomeError   = "error"
	OutcomeBudget  = "budget"
)

// feedKeep is how many recent entries per pane stay in memory.
const feedKeep = 50

// feedRotateBytes rotates feed.jsonl to feed.jsonl.1 at open time.
const feedRotateBytes = 5 << 20

type FeedEntry struct {
	Time    time.Time `json:"time"`
	PaneID  string    `json:"pane_id"`
	Dot     string    `json:"dot"`
	Turn    int       `json:"turn"`
	Outcome string    `json:"outcome"`
	Detail  string    `json:"detail,omitempty"`
}

type Feed struct {
	path   string
	mu     sync.Mutex
	recent map[string][]FeedEntry
}

func OpenFeed(path string) (*Feed, error) {
	f := &Feed{path: path, recent: map[string][]FeedEntry{}}
	if st, err := os.Stat(path); err == nil && st.Size() > feedRotateBytes {
		_ = os.Rename(path, path+".1")
	}
	file, err := os.Open(path)
	if err == nil {
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for sc.Scan() {
			var e FeedEntry
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				e.Detail = textutil.SanitizeModelText(e.Detail)
				f.remember(e)
			}
		}
		file.Close()
	}
	return f, nil
}

// remember keeps e in memory; e.Detail must already be sanitized.
func (f *Feed) remember(e FeedEntry) {
	list := append(f.recent[e.PaneID], e)
	if len(list) > feedKeep {
		list = list[len(list)-feedKeep:]
	}
	f.recent[e.PaneID] = list
}

// Append stores e. Detail often carries model or CLI output (error text), so
// control characters are stripped before it is kept, written or rendered.
func (f *Feed) Append(e FeedEntry) error {
	e.Detail = textutil.SanitizeModelText(e.Detail)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remember(e)
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(line, '\n'))
	return err
}

// Recent returns up to n entries for a pane, oldest first.
func (f *Feed) Recent(paneID string, n int) []FeedEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.recent[paneID]
	if len(list) > n {
		list = list[len(list)-n:]
	}
	return append([]FeedEntry(nil), list...)
}
