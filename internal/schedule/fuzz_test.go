package schedule

import (
	"testing"
	"time"
)

func FuzzScheduleParse(f *testing.F) {
	for _, s := range []string{"0 9 * * 1-5", "*/15 * * * *", "0 0 31 2 *", "61 * * * *", "", "* * * *", "0 0 * * 7", "5,10-12/2 * * * *"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		spec, err := Parse(expr)
		if err != nil {
			return
		}
		base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
		if next := spec.Next(base); !next.IsZero() && !next.After(base) {
			t.Fatalf("Next(%v) = %v for %q: not after", base, next, expr)
		}
		if prev := spec.Prev(base); !prev.IsZero() && !prev.Before(base) {
			t.Fatalf("Prev(%v) = %v for %q: not before", base, prev, expr)
		}
	})
}
