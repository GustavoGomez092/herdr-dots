package history

import (
	"errors"
	"slices"
	"time"

	"herdr-dots/internal/state"
)

// Stats summarizes one dot over a window, computed from its history file.
//
//   - Runs counts every run by kind (memory passes included).
//   - Verdicts counts non-memory runs that ended flagged, pass or suppressed;
//     Flagged and Suppressed are subsets of it.
//   - Resolved counts sent, edited, applied and dismissed resolutions;
//     Accepted is sent + edited + applied. Injected, discarded and orphaned
//     cards are neither.
//   - Errors counts runs that ended in an error (every kind).
//   - CostUSD sums the runs' known costs; CostUnknown counts runs whose cost
//     the backend could not report.
//   - MedianMs is the median latency of runs that did not fail (0 = none).
//   - Skipped counts unreadable lines in the whole file.
type Stats struct {
	Runs        map[string]int
	Verdicts    int
	Flagged     int
	Suppressed  int
	Errors      int
	Resolved    int
	Accepted    int
	Dismissed   int
	CostUSD     float64
	CostUnknown int
	MedianMs    int64
	Skipped     int
}

// TotalRuns is the number of runs of every kind.
func (st Stats) TotalRuns() int {
	n := 0
	for _, v := range st.Runs {
		n += v
	}
	return n
}

// FlaggedRate is Flagged / Verdicts (0 with no verdicts).
func (st Stats) FlaggedRate() float64 { return ratio(st.Flagged, st.Verdicts) }

// AcceptanceRate is Accepted / Resolved (0 with nothing resolved).
func (st Stats) AcceptanceRate() float64 { return ratio(st.Accepted, st.Resolved) }

// ErrorRate is Errors / TotalRuns (0 with no runs).
func (st Stats) ErrorRate() float64 { return ratio(st.Errors, st.TotalRuns()) }

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func inWindow(t, now time.Time, window time.Duration) bool {
	return !t.Before(now.Add(-window)) && !t.After(now)
}

// Stats computes slug's stats for [now-window, now].
func (s *Store) Stats(slug string, now time.Time, window time.Duration) (Stats, error) {
	runs, resolves, skipped, err := s.joined(slug)
	if err != nil {
		return Stats{Runs: map[string]int{}}, err
	}
	return statsFrom(runs, resolves, skipped, now, window), nil
}

// Profile is what the profile screen shows, read from one pass over the file.
type Profile struct {
	Week, Month Stats
	Rows        []Record // newest runs first, with resolutions
}

// Profile reads slug's file once and returns the 7- and 30-day stats and the
// newest rows runs (rows <= 0 = all).
func (s *Store) Profile(slug string, now time.Time, rows int) (Profile, error) {
	runs, resolves, skipped, err := s.joined(slug)
	if err != nil {
		return Profile{Week: Stats{Runs: map[string]int{}}, Month: Stats{Runs: map[string]int{}}}, err
	}
	p := Profile{
		Week:  statsFrom(runs, resolves, skipped, now, 7*24*time.Hour),
		Month: statsFrom(runs, resolves, skipped, now, 30*24*time.Hour),
	}
	for i := len(runs) - 1; i >= 0 && (rows <= 0 || len(p.Rows) < rows); i-- {
		p.Rows = append(p.Rows, runs[i])
	}
	return p, nil
}

func statsFrom(runs, resolves []Record, skipped int, now time.Time, window time.Duration) Stats {
	st := Stats{Runs: map[string]int{}, Skipped: skipped}
	var ms []int64
	for _, r := range runs {
		if !inWindow(r.At, now, window) {
			continue
		}
		st.Runs[r.Kind]++
		st.CostUSD += r.CostUSD
		if r.CostUnknown {
			st.CostUnknown++
		}
		switch r.Outcome {
		case state.OutcomeError:
			st.Errors++
		case state.OutcomeFlagged, state.OutcomePass, state.OutcomeSuppressed:
			if r.Kind != KindMemory {
				st.Verdicts++
				if r.Outcome == state.OutcomeFlagged {
					st.Flagged++
				}
				if r.Outcome == state.OutcomeSuppressed {
					st.Suppressed++
				}
			}
		}
		if r.Outcome != state.OutcomeError && r.Ms > 0 {
			ms = append(ms, r.Ms)
		}
	}
	for _, r := range resolves {
		if !inWindow(r.At, now, window) {
			continue
		}
		switch r.Resolution {
		case ResSent, ResEdited, ResApplied:
			st.Resolved++
			st.Accepted++
		case ResDismissed:
			st.Resolved++
			st.Dismissed++
		}
	}
	if len(ms) > 0 {
		slices.Sort(ms)
		if n := len(ms); n%2 == 1 {
			st.MedianMs = ms[n/2]
		} else {
			st.MedianMs = (ms[n/2-1] + ms[n/2]) / 2
		}
	}
	return st
}

// CostShare returns slug's share of every dot's cost in the window and the
// total. Unreadable files are skipped; their error is returned joined.
func (s *Store) CostShare(slug string, now time.Time, window time.Duration) (share, total float64, err error) {
	slugs, err := s.Slugs()
	if err != nil {
		return 0, 0, err
	}
	var mine float64
	var errs []error
	for _, other := range slugs {
		st, err := s.Stats(other, now, window)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		total += st.CostUSD
		if other == slug {
			mine = st.CostUSD
		}
	}
	if total > 0 {
		share = mine / total
	}
	return share, total, errors.Join(errs...)
}
