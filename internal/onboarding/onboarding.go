// Package onboarding keeps the first-run Setup screen's record,
// CONFIG_DIR/onboarding.json: what the developer answered about installing
// the MCP server and the keybindings. The screen asks on first use and
// applies only on y; the record decides when it asks again.
package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
)

// Version is the onboarding.json schema version this binary reads and
// writes (the "onboarding" migrate kind).
const Version = 1

// SnoozeFor is how long "not now" waits before asking again.
const SnoozeFor = 7 * 24 * time.Hour

// State is one item's answer.
type State string

const (
	Installed State = "installed"
	Snoozed   State = "snoozed"
	Never     State = "never"
)

// Topic is one thing the Setup screen offers.
type Topic string

const (
	MCP  Topic = "mcp"
	Keys Topic = "keys"
)

// Item is the record for one topic. Until (RFC3339) is set for Snoozed.
type Item struct {
	State State  `json:"state,omitempty"`
	Until string `json:"until,omitempty"`
}

// Record is onboarding.json.
type Record struct {
	Version int  `json:"version"`
	MCP     Item `json:"mcp"`
	Keys    Item `json:"keys"`
}

// ErrNewer: a newer herdr-dots wrote onboarding.json. It is neither used nor
// rewritten: the Setup screen stays closed.
var ErrNewer = fmt.Errorf("onboarding.json: %w", fsutil.ErrNewerVersion)

// ErrCorrupt: onboarding.json doesn't parse. It is never overwritten.
var ErrCorrupt = errors.New("onboarding.json is not valid JSON; fix or delete it")

// Item returns the record for t.
func (r Record) Item(t Topic) Item {
	if t == Keys {
		return r.Keys
	}
	return r.MCP
}

func (r *Record) set(t Topic, it Item) {
	if t == Keys {
		r.Keys = it
		return
	}
	r.MCP = it
}

// Asks reports whether the record lets the Setup screen ask about t at now:
// not after "never", and not while a snooze runs. Whether the item is
// actually missing is the caller's check.
func (r Record) Asks(t Topic, now time.Time) bool {
	it := r.Item(t)
	switch it.State {
	case Never:
		return false
	case Snoozed:
		until, err := time.Parse(time.RFC3339, it.Until)
		return err != nil || !now.Before(until)
	}
	return true
}

// Store reads and writes onboarding.json (paths.Paths.OnboardingFile).
type Store struct {
	Path        string
	Now         func() time.Time // nil = time.Now
	LockTimeout time.Duration    // 0 = 2s
}

func (s Store) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// Load reads the record. exists is false for a missing file: the first run.
// A newer file is ErrNewer, one that doesn't parse ErrCorrupt.
func (s Store) Load() (Record, bool, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{Version: Version}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return Record{}, true, ErrCorrupt
	}
	if head.Version > Version {
		return Record{}, true, fmt.Errorf("%w (version %d)", ErrNewer, head.Version)
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return Record{}, true, ErrCorrupt
	}
	return r, true, nil
}

// Mark records state for each topic, under <file>.lock, and writes the file
// atomically (0600). Snoozed sets Until to now + SnoozeFor. A newer or
// corrupt file is left as it is and returned as an error.
func (s Store) Mark(state State, topics ...Topic) error {
	timeout := s.LockTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	l, err := lockfile.Acquire(s.Path+".lock", timeout)
	if err != nil {
		return fmt.Errorf("onboarding.json: %w", err)
	}
	defer l.Release()
	r, _, err := s.Load()
	if err != nil {
		return err
	}
	it := Item{State: state}
	if state == Snoozed {
		it.Until = s.now().Add(SnoozeFor).UTC().Format(time.RFC3339)
	}
	for _, t := range topics {
		r.set(t, it)
	}
	r.Version = Version
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.Path, append(b, '\n'), 0o600)
}
