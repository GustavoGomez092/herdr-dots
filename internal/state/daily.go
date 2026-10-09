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

const dailyVersion = 1

// Day is one local day of background work across every Herdr session.
// Callers MUST treat Blocked, or any error from Daily.Today or Daily.Add, as
// "budget reached" and run nothing in the background: the cap never opens.
type Day struct {
	Date    string                    `json:"day"` // YYYY-MM-DD in the engine's local time
	Runs    map[string]map[string]int `json:"runs"`
	CostUSD float64                   `json:"cost_usd"`
	// Blocked: the day's file was unreadable and was moved aside. Callers
	// treat it as "budget reached" for the rest of this local day.
	Blocked bool `json:"blocked,omitempty"`
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

// dailyFile is the on-disk form. Cost accumulates as whole microdollars
// (CostMicro) so repeated adds never drift; cost_usd stays for readers of the
// v1 format, and is the fallback for files written before cost_micro existed.
type dailyFile struct {
	Version   int   `json:"version"`
	CostMicro int64 `json:"cost_micro"`
	Day
}

// maxCostUSD bounds one run's cost so the microdollar conversion cannot
// overflow int64 (and go negative) on a huge or garbage figure.
const maxCostUSD = 1e6

func microOf(usd float64) int64 {
	if math.IsNaN(usd) || usd <= 0 {
		return 0
	}
	if usd > maxCostUSD { // also catches +Inf
		usd = maxCostUSD
	}
	return int64(math.Round(usd * 1e6))
}

// Daily counts background runs and their dollar cost per local day in
// STATE_DIR/daily.json, shared by every Herdr session. Every read and update
// holds daily.json.lock, so engines in two sessions never lose an update. A
// new local date starts from zero. A corrupt file fails closed: it is moved
// aside and the rest of that local day is Blocked.
type Daily struct {
	path      string
	writeFile func(path string, data []byte, perm os.FileMode) error // nil = fsutil.WriteFileAtomic; tests inject failures
	mu        sync.Mutex                                             // queues this process's goroutines before the file lock
	Timeout   time.Duration                                          // lock wait; 0 = 2s
	Logf      func(format string, args ...any)                       // nil = silent
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

// read loads the file. Missing, or from an earlier date than today, is an
// empty day. A stored date later than today (the clock stepped backwards)
// keeps counting against the stored day. An unreadable file is moved aside
// and the rest of today is blocked: resetting it would make the cap unlimited.
func (d *Daily) read(today string) (dailyFile, error) {
	empty := dailyFile{Version: dailyVersion, Day: Day{Date: today, Runs: map[string]map[string]int{}}}
	data, err := os.ReadFile(d.path)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return dailyFile{}, err
	}
	var f dailyFile
	if err := json.Unmarshal(data, &f); err != nil {
		return d.quarantine(empty, err)
	}
	if f.Version > dailyVersion {
		return dailyFile{}, fmt.Errorf("%w (version %d)", ErrDailyVersion, f.Version)
	}
	if _, err := time.Parse("2006-01-02", f.Date); err != nil {
		return d.quarantine(empty, fmt.Errorf("bad day %q", f.Date))
	}
	if f.Date < today {
		return empty, nil
	}
	if f.Runs == nil {
		f.Runs = map[string]map[string]int{}
	}
	if f.CostMicro <= 0 {
		f.CostMicro = 0
		if c := f.CostUSD; !math.IsNaN(c) && !math.IsInf(c, 0) && c > 0 {
			f.CostMicro = microOf(c)
		}
	}
	return f, nil
}

func (d *Daily) encode(f dailyFile) ([]byte, error) {
	f.CostUSD = float64(f.CostMicro) / 1e6
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (d *Daily) write(f dailyFile) error {
	data, err := d.encode(f)
	if err != nil {
		return err
	}
	return d.writeAtomic(d.path, data, 0o600)
}

func (d *Daily) writeAtomic(path string, data []byte, perm os.FileMode) error {
	if d.writeFile != nil {
		return d.writeFile(path, data, perm)
	}
	return fsutil.WriteFileAtomic(path, data, perm)
}

// quarantine replaces an unreadable daily.json with a blocked record for
// today and keeps the bad file as daily.json.corrupt-<ts>. The order never
// leaves the cap open: the blocked record is written to a side file first,
// then the bad file is moved aside, then the side file takes its place. Any
// failure rolls back so either the bad file or the blocked file is at d.path,
// and the call errors.
func (d *Daily) quarantine(empty dailyFile, cause error) (dailyFile, error) {
	blocked := empty
	blocked.Blocked = true
	data, err := d.encode(blocked)
	if err != nil {
		return dailyFile{}, err
	}
	side := d.path + ".blocked"
	if err := d.writeAtomic(side, data, 0o600); err != nil {
		return dailyFile{}, fmt.Errorf("daily budget: cannot record block: %w", err)
	}
	aside := fmt.Sprintf("%s.corrupt-%d", d.path, time.Now().UnixNano())
	if err := os.Rename(d.path, aside); err != nil {
		_ = os.Remove(side)
		return dailyFile{}, err
	}
	if err := os.Rename(side, d.path); err != nil {
		_ = os.Rename(aside, d.path)
		_ = os.Remove(side)
		return dailyFile{}, err
	}
	if d.Logf != nil {
		d.Logf("daily budget: %s unreadable (%v); moved aside to %s, background runs blocked for %s", d.path, cause, aside, empty.Date)
	}
	return blocked, nil
}

// Today returns now's day: every dot's runs and the total cost so far.
func (d *Daily) Today(now time.Time) (Day, error) {
	var day Day
	err := d.locked(func() error {
		f, err := d.read(dayKey(now))
		if err != nil {
			return err
		}
		day = f.Day
		day.CostUSD = float64(f.CostMicro) / 1e6
		return nil
	})
	return day, err
}

// Add counts one run of kind for slug on now's day and adds its cost, rounded
// once to the nearest microdollar. A NaN, infinite or negative cost (or an
// unknown one, passed as 0) adds nothing. A blocked day stays blocked.
func (d *Daily) Add(slug, kind string, costUSD float64, now time.Time) error {
	var micro int64
	if !math.IsNaN(costUSD) && !math.IsInf(costUSD, 0) && costUSD > 0 {
		micro = microOf(costUSD)
	}
	return d.locked(func() error {
		f, err := d.read(dayKey(now))
		if err != nil {
			return err
		}
		if f.Runs[slug] == nil {
			f.Runs[slug] = map[string]int{}
		}
		f.Runs[slug][kind]++
		f.CostMicro += micro
		return d.write(f)
	})
}
