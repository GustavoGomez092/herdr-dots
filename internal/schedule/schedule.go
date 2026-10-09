// Package schedule parses standard 5-field cron expressions and computes
// slots in a time zone. It is pure: no clock, no goroutines, no I/O. The
// engine's scheduler sleeps until the next slot itself.
//
// Rules:
//   - fields: minute (0-59), hour (0-23), day of month (1-31), month (1-12 or
//     jan-dec), day of week (0-7 or sun-sat; 0 and 7 are Sunday);
//   - each field is a comma list of *, N, N-M, with an optional /step;
//   - @hourly, @daily (@midnight), @weekly, @monthly, @yearly (@annually);
//   - when both day fields are restricted (neither starts with *), a day
//     matches if either matches (classic cron); otherwise both must;
//   - slots are wall-clock times in the location of the time passed in. A
//     slot inside a skipped DST hour does not exist and does not run; a slot
//     inside a repeated DST hour runs once, at its first occurrence.
package schedule

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalid wraps every parse error.
var ErrInvalid = errors.New("invalid cron expression")

// maxYears bounds every search; Parse rejects expressions with no slot in it.
const maxYears = 5

// Spec is a parsed cron expression. The zero Spec never fires.
type Spec struct {
	expr                          string
	minute, hour, dom, month, dow uint64 // bit i set = value i allowed
	domStar, dowStar              bool
}

type field struct {
	name     string
	min, max int
	names    map[string]int
}

var (
	minuteF = field{name: "minute", min: 0, max: 59}
	hourF   = field{name: "hour", min: 0, max: 23}
	domF    = field{name: "day of month", min: 1, max: 31}
	monthF  = field{name: "month", min: 1, max: 12, names: map[string]int{
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}}
	dowF = field{name: "day of week", min: 0, max: 7, names: map[string]int{
		"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}}
)

var macros = map[string]string{
	"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *", "@monthly": "0 0 1 * *",
	"@weekly": "0 0 * * 0", "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@hourly": "0 * * * *",
}

// Parse reads a cron expression. It rejects expressions that never fire
// within five years (such as "0 0 31 4 *").
func Parse(expr string) (Spec, error) {
	raw := strings.TrimSpace(expr)
	src := raw
	if strings.HasPrefix(src, "@") {
		m, ok := macros[strings.ToLower(src)]
		if !ok {
			return Spec{}, fmt.Errorf("%w: unknown macro %q", ErrInvalid, src)
		}
		src = m
	}
	parts := strings.Fields(src)
	if len(parts) != 5 {
		return Spec{}, fmt.Errorf("%w: want 5 fields (minute hour day-of-month month day-of-week), got %d", ErrInvalid, len(parts))
	}
	s := Spec{expr: raw}
	var err error
	if s.minute, err = parseField(parts[0], minuteF); err != nil {
		return Spec{}, err
	}
	if s.hour, err = parseField(parts[1], hourF); err != nil {
		return Spec{}, err
	}
	if s.dom, err = parseField(parts[2], domF); err != nil {
		return Spec{}, err
	}
	if s.month, err = parseField(parts[3], monthF); err != nil {
		return Spec{}, err
	}
	if s.dow, err = parseField(parts[4], dowF); err != nil {
		return Spec{}, err
	}
	if s.dow&(1<<7) != 0 {
		s.dow |= 1 // 7 is Sunday too
	}
	s.domStar, s.dowStar = strings.HasPrefix(parts[2], "*"), strings.HasPrefix(parts[4], "*")
	if s.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
		return Spec{}, fmt.Errorf("%w: %q never fires", ErrInvalid, raw)
	}
	return s, nil
}

func parseField(text string, f field) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(text, ",") {
		if part == "" {
			return 0, fmt.Errorf("%w: empty item in %s %q", ErrInvalid, f.name, text)
		}
		rng, stepText, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("%w: %s step %q must be a whole number >= 1", ErrInvalid, f.name, stepText)
			}
			step = n
		}
		lo, hi := f.min, f.max
		switch {
		case rng == "*":
		case strings.Contains(rng, "-"):
			a, b, _ := strings.Cut(rng, "-")
			var err error
			if lo, err = value(a, f); err != nil {
				return 0, err
			}
			if hi, err = value(b, f); err != nil {
				return 0, err
			}
			if lo > hi {
				return 0, fmt.Errorf("%w: %s range %q runs backwards", ErrInvalid, f.name, rng)
			}
		default:
			v, err := value(rng, f)
			if err != nil {
				return 0, err
			}
			lo = v
			if !hasStep {
				hi = v // "5" is one value; "5/10" is 5, 15, 25, ...
			}
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, nil
}

func value(text string, f field) (int, error) {
	if v, ok := f.names[strings.ToLower(text)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(text)
	if err != nil || v < f.min || v > f.max {
		return 0, fmt.Errorf("%w: %s %q must be %d-%d", ErrInvalid, f.name, text, f.min, f.max)
	}
	return v, nil
}

// String returns the expression as written.
func (s Spec) String() string { return s.expr }

func (s Spec) dayMatches(t time.Time) bool {
	domOK := s.dom&(1<<uint(t.Day())) != 0
	dowOK := s.dow&(1<<uint(t.Weekday())) != 0
	if s.domStar || s.dowStar {
		return domOK && dowOK
	}
	return domOK || dowOK
}

// Next returns the first slot strictly after after, in after's location, or
// the zero time when there is none within five years.
func (s Spec) Next(after time.Time) time.Time {
	if s.minute == 0 {
		return time.Time{}
	}
	loc := after.Location()
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.AddDate(maxYears, 0, 0)
	for t.Before(limit) {
		y, mo, d := t.Date()
		h, mi, _ := t.Clock()
		switch {
		case s.month&(1<<uint(mo)) == 0:
			t = advance(t, time.Date(y, mo+1, 1, 0, 0, 0, 0, loc))
		case !s.dayMatches(t):
			t = advance(t, time.Date(y, mo, d+1, 0, 0, 0, 0, loc))
		case s.hour&(1<<uint(h)) == 0:
			t = advance(t, time.Date(y, mo, d, h+1, 0, 0, 0, loc))
		case s.minute&(1<<uint(mi)) == 0:
			t = t.Add(time.Minute)
		default:
			if c := canonical(t); c.After(after) {
				return c
			}
			t = t.Add(time.Minute) // the second occurrence of a repeated hour
		}
	}
	return time.Time{}
}

// Prev returns the last slot strictly before before (the same instants Next
// returns), or the zero time when there is none within five years.
func (s Spec) Prev(before time.Time) time.Time {
	if s.minute == 0 {
		return time.Time{}
	}
	loc := before.Location()
	y, mo, d := before.Date()
	for k := 0; k <= 366*maxYears; k++ {
		start := time.Date(y, mo, d-k, 0, 0, 0, 0, loc)
		end := time.Date(y, mo, d-k+1, 0, 0, 0, 0, loc)
		var last time.Time
		for t := s.Next(start.Add(-time.Second)); !t.IsZero() && t.Before(before) && t.Before(end); t = s.Next(t) {
			last = t
		}
		if !last.IsZero() {
			return last
		}
	}
	return time.Time{}
}

// advance moves to next, or one minute on if a DST transition made next land
// at or before cur (time.Date picks either side of an ambiguous wall time).
func advance(cur, next time.Time) time.Time {
	if next.After(cur) {
		return next
	}
	return cur.Add(time.Minute)
}

// canonical is the earliest instant showing the same wall-clock minute as t:
// in a repeated DST hour, the first occurrence.
func canonical(t time.Time) time.Time {
	best := t
	for _, d := range []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour} {
		if e := t.Add(-d); sameWall(e, t) {
			best = e
		}
	}
	return best
}

func sameWall(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	ah, ami, _ := a.Clock()
	bh, bmi, _ := b.Clock()
	return ay == by && am == bm && ad == bd && ah == bh && ami == bmi
}
