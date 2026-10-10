package engine

import (
	"testing"
	"time"

	"herdr-dots/internal/state"
)

// R2: a schedule over its daily cap logs the skip once a day per project and
// reason, not at every slot.
func TestACappedScheduleLogsItsSkipOnceADay(t *testing.T) {
	logs := &logSink{}
	r := newProRig(t, proOpts{cwd: t.TempDir(), deps: logs.deps}, scheduledDot("Security", "0 * * * *"))
	r.attach(t, "security")
	eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	for range maxScheduledPerDay {
		if err := r.daily.Add("security", state.RunScheduled, 0, r.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, next := range []string{"12:00", "13:00", "14:00"} {
		r.clock.Add(time.Hour)
		eventually(t, "the slot before "+next, func() bool { return r.dotState("security").Next == "scheduled Wed "+next })
		eventually(t, "the slot's fire to end", func() bool {
			r.e.mu.Lock()
			defer r.e.mu.Unlock()
			return r.e.bgQueued[key{schedPane, "security"}] == 0
		})
	}
	if n := len(scheduledRuns(r)); n != 0 {
		t.Fatalf("%d runs over the daily cap", n)
	}
	if n := logs.count("skipped (" + string(ReasonDailyRuns)); n != 1 {
		logs.mu.Lock()
		defer logs.mu.Unlock()
		t.Fatalf("the daily-cap skip was logged %d times over three slots, want 1:\n%v", n, logs.lines)
	}
}
