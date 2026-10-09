package history

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/state"
)

// fillHistory writes n lines spread over 30 days: three runs to every
// resolution, kinds and outcomes mixed like a busy dot.
func fillHistory(b testing.TB, s *Store, slug string, n int) {
	b.Helper()
	kinds := []string{KindReview, KindReview, KindReview, KindCouncil, KindAsk, KindResearch}
	outcomes := []string{state.OutcomePass, state.OutcomePass, state.OutcomeFlagged, state.OutcomeSuppressed}
	step := 30 * 24 * time.Hour / time.Duration(n)
	for i := 0; i < n; i++ {
		at := hNow.Add(-30 * 24 * time.Hour).Add(time.Duration(i) * step)
		card := CardKey("s", fmt.Sprint(i/4))
		r := Record{T: TypeRun, At: at, Dot: slug, Kind: kinds[i%len(kinds)], Outcome: outcomes[i%len(outcomes)],
			Card: card, Pane: "w1:p1", Turn: i, Ms: int64(5000 + i%4000), CostUSD: 0.004, In: 2300, Out: 180,
			Excerpt: "Session cookie is not rotated after a privilege change; rotate it in the login handler."}
		if i%4 == 3 {
			r = Record{T: TypeResolve, At: at, Dot: slug, Card: card, Resolution: ResDismissed}
		}
		if err := s.Append(r); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStats30d is V1: one full-size history file (20,000 lines) read
// once for the profile screen (7- and 30-day stats and 50 rows).
func BenchmarkStats30d(b *testing.B) {
	s := Open(filepath.Join(b.TempDir(), "history"))
	s.TrimEvery = 1 << 30 // keep all 20,000 lines
	fillHistory(b, s, "security", DefaultMaxLines)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := s.Profile("security", hNow, 50)
		if err != nil || len(p.Rows) != 50 || p.Month.TotalRuns() == 0 {
			b.Fatalf("profile %d rows, %d runs, %v", len(p.Rows), p.Month.TotalRuns(), err)
		}
	}
}

// BenchmarkCostShareTenDots is the profile's cost hint over ten full files.
func BenchmarkCostShareTenDots(b *testing.B) {
	s := Open(filepath.Join(b.TempDir(), "history"))
	s.TrimEvery = 1 << 30
	for d := 0; d < 10; d++ {
		fillHistory(b, s, fmt.Sprintf("dot-%d", d), DefaultMaxLines)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := s.CostShare("dot-3", hNow, 30*24*time.Hour); err != nil {
			b.Fatal(err)
		}
	}
}
