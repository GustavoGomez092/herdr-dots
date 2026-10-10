package state

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/textutil"
)

const (
	OutcomeFlagged = "flagged"
	OutcomePass    = "pass"
	OutcomeError   = "error"
	OutcomeBudget  = "budget"

	OutcomeLearned    = "learned"    // memory ops applied (detail "learned N")
	OutcomeSuppressed = "suppressed" // verdict too similar to remembered advice (detail "similar to d:m17")
)

// OutcomeCouncil: a council card was created (the entry's Dot is "council").
const OutcomeCouncil = "council"

// Proactive run outcomes (Phase 8). The entry's Turn is 0 and its Detail
// starts with the run's label ("research: …", "commit a1b2c3d: …").
const (
	OutcomeResearch  = "research"
	OutcomeScheduled = "scheduled"
	OutcomeCommit    = "commit"
)

// Draft outcomes (Phase 7): a patch card was created (detail: the diffstat),
// and a patch was applied to the working tree (detail: conflicts, if any).
const (
	OutcomeDrafted = "drafted"
	OutcomeApplied = "applied"
)

// OutcomeAsked: the main agent asked this dot (or a council, Dot "council")
// through the Dots MCP server (Phase 5). Detail starts with the severity, or
// "pass", followed by " · " and the run's cost detail when there is one.
const OutcomeAsked = "asked"

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
	needNL bool // the file ends in a truncated line: Append starts a new one
}

// chmod is os.Chmod; tests make it fail.
var chmod = os.Chmod

func OpenFeed(path string) (*Feed, error) { return OpenFeedLogged(path, nil) }

// OpenFeedLogged is OpenFeed that logs (once) a feed it could not make
// owner-only; logf nil = silent.
func OpenFeedLogged(path string, logf func(format string, args ...any)) (*Feed, error) {
	f := &Feed{path: path, recent: map[string][]FeedEntry{}}
	// The feed quotes model output: an older file made with a looser mode
	// becomes owner-only (O_CREATE's 0600 only applies to new files). This
	// runs before the rotation, so a rotated feed is owner-only too, and it
	// covers a feed.jsonl.1 rotated by an older version.
	var loose []string
	for _, p := range []string{path, path + ".1"} {
		if st, err := os.Stat(p); err == nil && st.Mode().Perm()&0o077 != 0 {
			if err := chmod(p, 0o600); err != nil {
				loose = append(loose, err.Error())
			}
		}
	}
	if len(loose) > 0 && logf != nil {
		logf("feed: could not make the feed owner-only: %s", strings.Join(loose, "; "))
	}
	if st, err := os.Stat(path); err == nil && st.Size() > feedRotateBytes {
		if err := os.Rename(path, path+".1"); err != nil && logf != nil {
			logf("feed: could not rotate %s (%v); appending to it", path, err)
		}
	}
	file, err := os.Open(path)
	if err == nil {
		bad := 0
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for sc.Scan() {
			var e FeedEntry
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				e.Detail = textutil.SanitizeModelText(e.Detail)
				f.remember(e)
			} else if len(bytes.TrimSpace(sc.Bytes())) > 0 {
				bad++
			}
		}
		if logf != nil && bad > 0 {
			logf("feed: skipped %d unreadable line(s) in %s (a line cut short by a crash?)", bad, path)
		}
		if logf != nil && sc.Err() != nil {
			logf("feed: stopped reading %s early: %v", path, sc.Err())
		}
		// A last line without its newline (a crash mid-write): the next
		// entry starts on a new line instead of being glued onto it.
		last := make([]byte, 1)
		if n, _ := file.ReadAt(last, max(sizeOf(file)-1, 0)); n == 1 && last[0] != '\n' {
			f.needNL = true
		}
		file.Close()
	}
	return f, nil
}

func sizeOf(file *os.File) int64 {
	if st, err := file.Stat(); err == nil {
		return st.Size()
	}
	return 0
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
	if f.needNL {
		line = append([]byte{'\n'}, line...)
	}
	if _, err = file.Write(append(line, '\n')); err != nil {
		return err
	}
	f.needNL = false
	return nil
}

// Recent returns up to n entries for a pane, oldest first.
func (f *Feed) Recent(paneID string, n int) []FeedEntry {
	if n <= 0 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.recent[paneID]
	if len(list) > n {
		list = list[len(list)-n:]
	}
	return append([]FeedEntry(nil), list...)
}
