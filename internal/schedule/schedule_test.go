package schedule

import (
	"errors"
	"sort"
	"strconv"
	"testing"
	"time"
	_ "time/tzdata" // the DST tests need America/New_York on every OS
)

func nyc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tz database: %v", err)
	}
	return loc
}

func mustParse(t *testing.T, expr string) Spec {
	t.Helper()
	s, err := Parse(expr)
	if err != nil {
		t.Fatalf("Parse(%q): %v", expr, err)
	}
	return s
}

func TestParseAcceptsStandardForms(t *testing.T) {
	for _, expr := range []string{
		"0 9 * * 1-5", "*/15 * * * *", "0 0 1 * *", "@daily", "@HOURLY", "0 9 * * mon-fri",
		"0 9 * JAN *", "0 0 * * 7", "5,35 8-18/2 * * *", "0 0 29 2 *", "5/20 * * * *",
	} {
		s := mustParse(t, expr)
		if s.String() != expr {
			t.Errorf("String() = %q, want %q", s.String(), expr)
		}
	}
}

func TestParseRejectsBadExpressions(t *testing.T) {
	for _, expr := range []string{
		"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *",
		"* * * * 8", "*/0 * * * *", "5-1 * * * *", "a * * * *", "1,,2 * * * *", "0 0 31 4 *", "@reboot",
	} {
		if _, err := Parse(expr); !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q) = %v, want ErrInvalid", expr, err)
		}
	}
}

func TestNextWeekdaysSkipTheWeekend(t *testing.T) {
	loc := nyc(t)
	s := mustParse(t, "0 9 * * 1-5")
	got := s.Next(time.Date(2026, 10, 23, 10, 0, 0, 0, loc)) // Friday after 9:00
	if want := time.Date(2026, 10, 26, 9, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("Next = %v, want %v", got, want)
	}
	if got.Location() != loc {
		t.Fatalf("location %v, want %v", got.Location(), loc)
	}
}

func TestNextIsStrictlyAfter(t *testing.T) {
	s := mustParse(t, "*/15 * * * *")
	at := time.Date(2026, 10, 21, 9, 15, 0, 0, time.UTC)
	if got := s.Next(at); !got.Equal(at.Add(15 * time.Minute)) {
		t.Fatalf("Next(slot) = %v", got)
	}
	if got := s.Next(at.Add(-time.Nanosecond)); !got.Equal(at) {
		t.Fatalf("Next(just before) = %v", got)
	}
}

func TestNextMonthEndsAndLeapDays(t *testing.T) {
	if got, want := mustParse(t, "0 0 31 * *").Next(time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)),
		time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("31st: %v, want %v", got, want)
	}
	if got, want := mustParse(t, "0 0 29 2 *").Next(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
		time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("Feb 29: %v, want %v", got, want)
	}
}

func TestDayOfMonthOrDayOfWeekWhenBothRestricted(t *testing.T) {
	s := mustParse(t, "0 0 13 * 5") // the 13th, or any Friday
	if got, want := s.Next(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("Next = %v, want Friday %v", got, want)
	}
	s = mustParse(t, "0 0 */2 * 5") // a * field: both must match
	if got, want := s.Next(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("Next = %v, want %v (odd day and Friday)", got, want)
	}
}

func TestSpringForwardSkipsTheMissingSlot(t *testing.T) {
	loc := nyc(t) // 2026-03-08 02:00 EST -> 03:00 EDT
	day := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	if got, want := mustParse(t, "30 2 * * *").Next(day), time.Date(2026, 3, 9, 2, 30, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("02:30 on the spring-forward day: %v, want the next day %v", got, want)
	}
	if got, want := mustParse(t, "0 3 * * *").Next(day), time.Date(2026, 3, 8, 3, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("03:00: %v, want %v", got, want)
	}
}

func TestFallBackRunsTheRepeatedSlotOnce(t *testing.T) {
	loc := nyc(t) // 2026-11-01 02:00 EDT -> 01:00 EST: 01:00-01:59 happens twice
	s := mustParse(t, "30 1 * * *")
	first := s.Next(time.Date(2026, 10, 31, 12, 0, 0, 0, loc))
	if want := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC); !first.Equal(want) {
		t.Fatalf("first = %v, want 01:30 EDT (%v)", first, want)
	}
	second := s.Next(first)
	if want := time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC); !second.Equal(want) {
		t.Fatalf("after the first 01:30 = %v, want 01:30 EST the next day (%v)", second, want)
	}
	// From inside the repeated hour (01:10 EST), the slot already ran.
	inside := time.Date(2026, 11, 1, 6, 10, 0, 0, time.UTC).In(loc)
	if got := s.Next(inside); !got.Equal(second) {
		t.Fatalf("Next(01:10 EST) = %v, want %v", got, second)
	}
	hourly := mustParse(t, "0 * * * *")
	h1 := hourly.Next(time.Date(2026, 11, 1, 0, 30, 0, 0, loc))
	h2 := hourly.Next(h1)
	if !h1.Equal(time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)) || !h2.Equal(time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("hourly across fall-back: %v then %v, want 01:00 EDT then 02:00 EST", h1, h2)
	}
}

func TestPrevMatchesNext(t *testing.T) {
	loc := nyc(t)
	s := mustParse(t, "0 9 * * 1-5")
	mon := time.Date(2026, 10, 26, 8, 0, 0, 0, loc)
	if got, want := s.Prev(mon), time.Date(2026, 10, 23, 9, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("Prev = %v, want Friday %v", got, want)
	}
	slot := time.Date(2026, 10, 26, 9, 0, 0, 0, loc)
	if got, want := s.Prev(slot), time.Date(2026, 10, 23, 9, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("Prev(slot) = %v, want the slot before (%v)", got, want)
	}
	fb := mustParse(t, "30 1 * * *")
	after := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC).In(loc) // 03:00 EST
	if got, want := fb.Prev(after), fb.Next(time.Date(2026, 10, 31, 12, 0, 0, 0, loc)); !got.Equal(want) {
		t.Fatalf("Prev across fall-back = %v, want the first occurrence %v", got, want)
	}
	if got := mustParse(t, "0 0 29 2 *").Prev(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)); !got.Equal(time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Prev leap day = %v", got)
	}
}

func TestZeroSpecNeverFires(t *testing.T) {
	var s Spec
	if !s.Next(time.Now()).IsZero() || !s.Prev(time.Now()).IsZero() {
		t.Fatal("zero Spec must never fire")
	}
}

// --- property-style checks against a brute-force oracle ---

var propZones = []string{"America/New_York", "Europe/London", "Australia/Lord_Howe", "Asia/Tokyo", "UTC"}

func loadZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no tz database for %s: %v", name, err)
	}
	return loc
}

func sameWallMinute(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	ah, ai, _ := a.Clock()
	bh, bi, _ := b.Clock()
	return ay == by && am == bm && ad == bd && ah == bh && ai == bi
}

// oracleSlots lists, minute by minute over [from, to), every real instant whose
// wall clock matches the spec and which is the first occurrence of that wall
// minute (so skipped wall times never appear and repeated ones appear once).
func oracleSlots(s Spec, loc *time.Location, from, to time.Time) []time.Time {
	var out []time.Time
	for t := from.In(loc); t.Before(to); t = t.Add(time.Minute) {
		h, mi, _ := t.Clock()
		if s.month&(1<<uint(t.Month())) == 0 || !s.dayMatches(t) ||
			s.hour&(1<<uint(h)) == 0 || s.minute&(1<<uint(mi)) == 0 {
			continue
		}
		first := true
		// A wall minute repeats only after a backward shift of 30 or 60 minutes.
		for back := 30; back <= 180; back += 30 {
			if sameWallMinute(t.Add(-time.Duration(back)*time.Minute), t) {
				first = false
				break
			}
		}
		if first {
			out = append(out, t)
		}
	}
	return out
}

func TestNextAndPrevMatchBruteForceAcrossZonesAndDST(t *testing.T) {
	if testing.Short() {
		t.Skip("brute-force oracle sweep")
	}
	exprs := []string{"30 2 * * *", "*/30 * * * *", "15 1 * * *", "45 0-3 * * 0"}
	d := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }
	// Windows around each zone's 2026 transitions (a no-DST zone gets a quiet one).
	windows := map[string][][2]time.Time{
		"America/New_York":    {{d(3, 2), d(3, 16)}, {d(10, 26), d(11, 9)}},
		"Europe/London":       {{d(3, 23), d(4, 6)}, {d(10, 19), d(11, 2)}},
		"Australia/Lord_Howe": {{d(3, 30), d(4, 13)}, {d(9, 28), d(10, 12)}},
		"Asia/Tokyo":          {{d(3, 2), d(3, 16)}},
		"UTC":                 {{d(10, 26), d(11, 9)}},
	}
	for _, zone := range propZones {
		loc := loadZone(t, zone)
		for _, expr := range exprs {
			s := mustParse(t, expr)
			for _, w := range windows[zone] {
				checkAgainstOracle(t, zone, expr, s, loc, w[0], w[1])
			}
		}
	}
}

// nearTransition reports whether the UTC offset changes within a day of t.
func nearTransition(t time.Time, loc *time.Location) bool {
	_, a := t.Add(-24 * time.Hour).In(loc).Zone()
	_, b := t.Add(24 * time.Hour).In(loc).Zone()
	return a != b
}

func checkAgainstOracle(t *testing.T, zone, expr string, s Spec, loc *time.Location, from, to time.Time) {
	t.Helper()
	slots := oracleSlots(s, loc, from, to)
	if len(slots) == 0 {
		t.Fatalf("%s %q: oracle found no slots", zone, expr)
	}
	// Sample points: slots and a spread of offsets around them, kept away
	// from the window edges so the oracle is complete.
	lo, hi := from.AddDate(0, 0, 3), to.AddDate(0, 0, -3)
	var samples []time.Time
	for i, sl := range slots {
		if sl.Before(lo) || sl.After(hi) {
			continue
		}
		samples = append(samples, sl, sl.Add(-time.Nanosecond), sl.Add(time.Nanosecond))
		if i%7 == 0 {
			samples = append(samples, sl.Add(17*time.Minute), sl.Add(-41*time.Minute), sl.Add(90*time.Minute+13*time.Second))
		}
	}
	for _, x := range samples {
		x = x.In(loc)
		i := sort.Search(len(slots), func(i int) bool { return slots[i].After(x) })
		if i >= len(slots) {
			continue // the next slot lies beyond the oracle window
		}
		if got := s.Next(x); !got.Equal(slots[i]) || got.Location() != loc {
			t.Fatalf("%s %q Next(%v) = %v, want %v", zone, expr, x, got, slots[i])
		}
		j := sort.Search(len(slots), func(i int) bool { return !slots[i].Before(x) }) - 1
		if j < 0 {
			continue // x is at or before the first slot of the window
		}
		if got := s.Prev(x); !got.Equal(slots[j]) {
			t.Fatalf("%s %q Prev(%v) = %v, want %v", zone, expr, x, got, slots[j])
		}
	}
}

func TestDSTTransitionsEveryZone(t *testing.T) {
	// Per zone: a wall time inside the spring-forward gap never fires on that
	// day; one inside the fall-back overlap fires exactly once that day.
	type ymd struct{ y, m, d int }
	cases := []struct {
		zone         string
		gapDay       ymd
		gapH, gapM   int
		overDay      ymd
		overH, overM int
	}{
		{"America/New_York", ymd{2026, 3, 8}, 2, 30, ymd{2026, 11, 1}, 1, 30},
		{"Europe/London", ymd{2026, 3, 29}, 1, 30, ymd{2026, 10, 25}, 1, 30},
		// Lord Howe shifts by 30 minutes: 2026-10-04 02:00 -> 02:30, 2026-04-05 02:00 -> 01:30.
		{"Australia/Lord_Howe", ymd{2026, 10, 4}, 2, 15, ymd{2026, 4, 5}, 1, 45},
	}
	for _, c := range cases {
		loc := loadZone(t, c.zone)
		gap := time.Date(c.gapDay.y, time.Month(c.gapDay.m), c.gapDay.d, c.gapH, c.gapM, 0, 0, loc)
		if gap.Hour() == c.gapH && gap.Minute() == c.gapM {
			t.Fatalf("%s: test setup, %02d:%02d exists on the gap day", c.zone, c.gapH, c.gapM)
		}
		spec := mustParse(t, strconv.Itoa(c.gapM)+" "+strconv.Itoa(c.gapH)+" * * *")
		dayStart := time.Date(c.gapDay.y, time.Month(c.gapDay.m), c.gapDay.d, 0, 0, 0, 0, loc)
		next := spec.Next(dayStart.Add(-time.Second))
		if y, m, d := next.Date(); y == c.gapDay.y && int(m) == c.gapDay.m && d == c.gapDay.d {
			t.Errorf("%s: skipped slot fired on the gap day: %v", c.zone, next)
		}
		if h, mi, _ := next.Clock(); h != c.gapH || mi != c.gapM {
			t.Errorf("%s: next gap-slot %v has the wrong wall time", c.zone, next)
		}

		spec = mustParse(t, strconv.Itoa(c.overM)+" "+strconv.Itoa(c.overH)+" * * *")
		dayStart = time.Date(c.overDay.y, time.Month(c.overDay.m), c.overDay.d, 0, 0, 0, 0, loc)
		dayEnd := time.Date(c.overDay.y, time.Month(c.overDay.m), c.overDay.d+1, 0, 0, 0, 0, loc)
		count := 0
		var last time.Time
		for n := spec.Next(dayStart.Add(-time.Second)); n.Before(dayEnd); n = spec.Next(n) {
			count++
			last = n
		}
		if count != 1 {
			t.Errorf("%s: repeated slot fired %d times on the fall-back day, want 1", c.zone, count)
			continue
		}
		// First occurrence: neither 30 minutes nor an hour earlier shows the same wall time.
		for _, back := range []time.Duration{30 * time.Minute, time.Hour} {
			if sameWallMinute(last.Add(-back), last) {
				t.Errorf("%s: %v is not the first occurrence", c.zone, last)
			}
		}
	}
}

func TestPrevOfNextRoundTripsEverywhere(t *testing.T) {
	exprs := []string{"0 9 * * 1-5", "*/20 * * * *", "30 1 * * *", "0 0 1 * *", "0 0 13 * 5"}
	for _, zone := range propZones {
		loc := loadZone(t, zone)
		for _, expr := range exprs {
			s := mustParse(t, expr)
			x := time.Date(2026, 2, 1, 0, 7, 0, 0, loc)
			for i := 0; i < 400; i++ {
				n := s.Next(x)
				if n.IsZero() || !n.After(x) {
					t.Fatalf("%s %q: Next(%v) = %v, not strictly after", zone, expr, x, n)
				}
				if p := s.Prev(n.Add(time.Nanosecond)); !p.Equal(n) {
					t.Fatalf("%s %q: Prev(Next(%v)+1ns) = %v, want %v", zone, expr, x, p, n)
				}
				p := s.Prev(n)
				if !p.Before(n) {
					t.Fatalf("%s %q: Prev(%v) = %v, not strictly before", zone, expr, n, p)
				}
				if nn := s.Next(p); !nn.Equal(n) {
					t.Fatalf("%s %q: Next(Prev(%v)) = %v", zone, expr, n, nn)
				}
				x = n.Add(time.Duration(i%5) * 37 * time.Minute)
			}
		}
	}
}

func TestImpossibleExpressionTerminates(t *testing.T) {
	if _, err := Parse("0 0 31 2 *"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Parse(Feb 31) = %v, want ErrInvalid", err)
	}
	// Bypass Parse's check to prove Next and Prev are bounded on their own.
	s := Spec{expr: "0 0 31 2 *", minute: 1, hour: 1, dom: 1 << 31, month: 1 << 2, dow: 0xff, dowStar: true}
	done := make(chan [2]time.Time, 1)
	go func() {
		now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		done <- [2]time.Time{s.Next(now), s.Prev(now)}
	}()
	select {
	case got := <-done:
		if !got[0].IsZero() || !got[1].IsZero() {
			t.Fatalf("Next/Prev = %v, want zero times", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Next or Prev did not terminate on an impossible expression")
	}
}
