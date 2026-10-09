package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
)

// Background run kinds counted by Daily.
const (
	RunResearch  = "research"  // idle research pass
	RunScheduled = "scheduled" // a cron slot
	RunCommit    = "commit"    // a new HEAD in an attached pane's repo
)

// ErrDailyVersion: daily.json was written by a newer herdr-dots.
var ErrDailyVersion = errors.New("daily.json written by a newer herdr-dots")

// ErrDailyCorrupt: daily.json is unreadable. The daily budget fails closed:
// callers must treat this error as "budget reached" and run nothing in the
// background. The file is left untouched; delete it to reset the counter.
var ErrDailyCorrupt = errors.New("daily.json is unreadable; background runs are blocked until it is deleted")

const dailyVersion = 1

// Day is one local day of background work across every Herdr session.
type Day struct {
	Date    string                    `json:"day"` // YYYY-MM-DD in the engine's local time
	Runs    map[string]map[string]int `json:"runs"`
	CostUSD float64                   `json:"cost_usd"`
}

// Count is slug's runs of kind on this day.
func (d Day) Count(slug, kind string) int { return d.Runs[slug][kind] }

// Total is every run of every dot on this day.
func (d Day) Total() int {
	n := 0
	for _, kinds := range d.Runs {
		for _, c := range kinds {
			n += c
		}
	}
	return n
}

type dailyFile struct {
	Version int `json:"version"`
	Day
}

// Daily counts background runs and their dollar cost per local day in
// STATE_DIR/daily.json, shared by every Herdr session. Every read and update
// holds daily.json.lock, so engines in two sessions never lose an update. A
// new local date starts from zero. A corrupt file fails closed (ErrDailyCorrupt).
type Daily struct {
	path    string
	mu      sync.Mutex                       // queues this process's goroutines before the file lock
	Timeout time.Duration                    // lock wait; 0 = 2s
	Logf    func(format string, args ...any) // nil = silent
}

// OpenDaily returns the store for path, creating its directory (0700).
func OpenDaily(path string) (*Daily, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &Daily{path: path}, nil
}

func dayKey(now time.Time) string { return now.Format("2006-01-02") }

func (d *Daily) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return 2 * time.Second
}

func (d *Daily) locked(fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	l, err := lockfile.Acquire(d.path+".lock", d.timeout())
	if err != nil {
		return fmt.Errorf("daily budget file: %w", err)
	}
	defer l.Release()
	return fn()
}

// read loads the file for today. A missing file, or one from an earlier
// date, is an empty day. An unreadable one fails closed with ErrDailyCorrupt
// (logged, file left in place): resetting it would make the cap unlimited.
func (d *Daily) read(today string) (Day, error) {
	empty := Day{Date: today, Runs: map[string]map[string]int{}}
	data, err := os.ReadFile(d.path)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return Day{}, err
	}
	var f dailyFile
	if err := json.Unmarshal(data, &f); err != nil {
		if d.Logf != nil {
			d.Logf("daily budget: %s unreadable (%v); background runs blocked until it is deleted", d.path, err)
		}
		return Day{}, ErrDailyCorrupt
	}
	if f.Version > dailyVersion {
		return Day{}, fmt.Errorf("%w (version %d)", ErrDailyVersion, f.Version)
	}
	if f.Date != today {
		return empty, nil
	}
	if f.Runs == nil {
		f.Runs = map[string]map[string]int{}
	}
	if math.IsNaN(f.CostUSD) || math.IsInf(f.CostUSD, 0) || f.CostUSD < 0 {
		f.CostUSD = 0
	}
	return f.Day, nil
}

// Today returns now's day: every dot's runs and the total cost so far.
func (d *Daily) Today(now time.Time) (Day, error) {
	var day Day
	err := d.locked(func() error {
		var err error
		day, err = d.read(dayKey(now))
		return err
	})
	return day, err
}

// Add counts one run of kind for slug on now's day and adds its cost. A NaN,
// infinite or negative cost (or an unknown one, passed as 0) adds nothing.
func (d *Daily) Add(slug, kind string, costUSD float64, now time.Time) error {
	if math.IsNaN(costUSD) || math.IsInf(costUSD, 0) || costUSD < 0 {
		costUSD = 0
	}
	return d.locked(func() error {
		day, err := d.read(dayKey(now))
		if err != nil {
			return err
		}
		if day.Runs[slug] == nil {
			day.Runs[slug] = map[string]int{}
		}
		day.Runs[slug][kind]++
		day.CostUSD += costUSD
		data, err := json.MarshalIndent(dailyFile{Version: dailyVersion, Day: day}, "", "  ")
		if err != nil {
			return err
		}
		return fsutil.WriteFileAtomic(d.path, append(data, '\n'), 0o600)
	})
}
