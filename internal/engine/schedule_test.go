package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // the DST tests need America/New_York on every OS

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// scheduledDot runs its task at cron on the projects it was attached to.
func scheduledDot(name, cron string) dots.Dot {
	d := dot(name, dots.ModeQueue)
	d.Trigger.OnTurn = false
	d.Schedule.Cron, d.Schedule.Task = cron, "Review commits since the last run for security issues."
	return d
}

// at is a time on proStart's day (a Wednesday), local time.
func at(h, m int) time.Time { return time.Date(2026, 10, 21, h, m, 0, 0, time.Local) }

// knownProject records testProject (rooted at root) with dot attached recently.
func knownProject(root, slug string, seen time.Time) func(*memory.Store) {
	return func(m *memory.Store) {
		_ = m.SeeProject(memory.Project{ID: testProject, Name: "repo", Root: root})
		_ = m.SeeDots(testProject, []string{slug}, seen)
	}
}

func scheduledRuns(r *proRig) []backend.Request {
	var out []backend.Request
	for _, c := range r.be.runs(backend.PassReview) {
		if c.Trigger == backend.TriggerScheduled {
			out = append(out, c)
		}
	}
	return out
}

func TestScheduledSlotRunsOnceOnTheAttachedPane(t *testing.T) {
	root := t.TempDir()
	r := newProRig(t, proOpts{cwd: root}, scheduledDot("Security", "0 11 * * *"))
	r.attach(t, "security")
	eventually(t, "roster shows the next slot", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	r.be.set(proResult("warn", "Commit 3f2a skips the CSRF check."))
	r.clock.Add(time.Hour + 20*time.Second)
	eventually(t, "the scheduled run", func() bool { return len(scheduledRuns(r)) == 1 })
	req := scheduledRuns(r)[0]
	if !strings.Contains(req.Question, "Review commits since the last run") || !strings.Contains(req.Question, "Since: never") || req.Cwd != root || !req.RepoRead {
		t.Fatalf("request %+v", req)
	}
	eventually(t, "a card on the attached pane", func() bool { return len(r.q.Pending(pane)) == 1 })
	if c := r.q.Pending(pane)[0]; c.Label != "scheduled Wed 11:00" || c.Origin != state.RunScheduled || c.Mode != "queue" {
		t.Fatalf("card %+v", c)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 1 {
		t.Fatalf("%d runs for one slot", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.Equal(at(11, 0)) {
		t.Fatalf("claimed %v", last)
	}
	r.clock.Add(24 * time.Hour)
	eventually(t, "the next day's slot", func() bool { return len(scheduledRuns(r)) == 2 })
	if q := scheduledRuns(r)[1].Question; !strings.Contains(q, "Since: Wed 21 Oct 2026 11:00") {
		t.Fatalf("second run's since: %s", q)
	}
}

// Review Focus 2: two Herdr sessions, one memory dir, one scheduled dot on
// one project: exactly one run per slot.
func TestTwoEnginesRunASlotOnce(t *testing.T) {
	shared, root := t.TempDir(), t.TempDir()
	clk := &proClock{t: at(10, 59)}
	d := scheduledDot("Security", "0 11 * * *")
	a := newProRig(t, proOpts{shared: shared, clock: clk, cwd: root}, d)
	b := newProRig(t, proOpts{shared: shared, clock: clk, cwd: root}, d)
	a.attach(t, "security")
	b.attach(t, "security")
	time.Sleep(60 * time.Millisecond)
	// Both engines pass every pre-check and queue a job before either claims:
	// the loser must be refused by the claim itself.
	a.e.bgSem <- struct{}{}
	b.e.bgSem <- struct{}{}
	clk.Add(90 * time.Second)
	eventually(t, "both engines queue the slot", func() bool { return queuedFor(a) > 0 && queuedFor(b) > 0 })
	<-a.e.bgSem
	<-b.e.bgSem
	eventually(t, "one run", func() bool { return len(scheduledRuns(a))+len(scheduledRuns(b)) == 1 })
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(a)) + len(scheduledRuns(b)); n != 1 {
		t.Fatalf("%d runs across two engines", n)
	}
}

// Review Focus 1: a slot missed while Herdr was closed runs once at start
// within catch_up_hours; an older one is skipped.
func TestCatchUpRunsOnceWithinTheWindow(t *testing.T) {
	root := t.TempDir()
	prev := func(m *memory.Store) {
		knownProject(root, "security", at(8, 0))(m)
		_, _ = m.ClaimSchedule(testProject, "security", at(9, 0).AddDate(0, 0, -1), at(9, 0).AddDate(0, 0, -1)) // ran yesterday
	}
	d := scheduledDot("Security", "0 9 * * *")
	r := newProRig(t, proOpts{clock: &proClock{t: at(15, 0)}, prepare: prev}, d)
	eventually(t, "a catch-up run at start", func() bool { return len(scheduledRuns(r)) == 1 })
	if q := scheduledRuns(r)[0].Question; !strings.Contains(q, "Since: Tue 20 Oct 2026 09:00") {
		t.Fatalf("since %s", q)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 1 {
		t.Fatalf("%d catch-up runs", n)
	}
	logs := &logSink{}
	stale := newProRig(t, proOpts{clock: &proClock{t: at(22, 0)}, prepare: prev, deps: logs.deps}, d) // 13 h after the slot
	eventually(t, "the stale slot is logged", func() bool { return logs.count("not caught up") == 1 })
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(stale)); n != 0 {
		t.Fatalf("a slot older than catch_up_hours ran %d times", n)
	}
}

// logSink collects an engine's log lines.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) deps(d *Deps) {
	d.Logf = func(f string, a ...any) {
		l.mu.Lock()
		l.lines = append(l.lines, fmt.Sprintf(f, a...))
		l.mu.Unlock()
	}
}

func (l *logSink) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

// queuedFor is the rig pane's queued background jobs for "security".
func queuedFor(r *proRig) int {
	r.e.mu.Lock()
	defer r.e.mu.Unlock()
	return r.e.bgQueued[key{pane, "security"}]
}

// A schedule that never ran starts at its next slot: no retroactive run.
func TestNewScheduleDoesNotCatchUp(t *testing.T) {
	root := t.TempDir()
	r := newProRig(t, proOpts{clock: &proClock{t: at(10, 0)}, prepare: knownProject(root, "security", at(8, 0))},
		scheduledDot("Security", "0 9 * * *"))
	eventually(t, "the passed slot is recorded", func() bool {
		last, _ := r.mem.LastSchedule(testProject, "security")
		return last.Equal(at(9, 0))
	})
	time.Sleep(80 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 0 {
		t.Fatalf("%d retroactive runs", n)
	}
}

// Review Focus 1: a laptop asleep through several slots runs only the
// latest one on wake, not a burst.
func TestWakeAfterSleepRunsOnlyTheLatestSlot(t *testing.T) {
	root := t.TempDir()
	r := newProRig(t, proOpts{cwd: root}, scheduledDot("Security", "0 * * * *"))
	r.attach(t, "security")
	time.Sleep(60 * time.Millisecond)
	r.clock.Add(5*time.Hour + 10*time.Minute) // 10:00 -> 15:10, slots 11:00 ... 15:00 passed
	eventually(t, "one run", func() bool { return len(scheduledRuns(r)) == 1 })
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 1 {
		t.Fatalf("%d runs after sleeping through 5 slots", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.Equal(at(15, 0)) {
		t.Fatalf("ran slot %v, want 15:00", last)
	}
}

// An explicit project with no attached pane: the result waits in the inbox,
// with a toast. A missing project root is skipped with one toast a day.
func TestExplicitProjectsInboxAndMissingRoot(t *testing.T) {
	root := t.TempDir()
	d := scheduledDot("Security", "0 11 * * *")
	missing := filepath.Join(t.TempDir(), "gone")
	d.Schedule.Projects = []string{root, missing}
	r := newProRig(t, proOpts{}, d)
	time.Sleep(60 * time.Millisecond) // planned at 10:00: 11:00 is a normal slot, not a catch-up
	r.be.set(proResult("warn", "The deploy script pins an old base image."))
	r.clock.Add(time.Hour + 20*time.Second)
	eventually(t, "an inbox card", func() bool { cs, _ := r.inbox.List(testProject); return len(cs) == 1 })
	toasts := strings.Join(r.toasts(), "\n")
	if !strings.Contains(toasts, "Security (scheduled) has a finding for repo") || strings.Count(toasts, missing+" is not on this machine") != 1 {
		t.Fatalf("toasts:\n%s", toasts)
	}
	r.clock.Add(time.Hour) // another slot today (cron edited below): still one missing-root toast
	r.setDots(func() dots.Dot { x := d; x.Schedule.Cron = "0 12 * * *"; return x }())
	eventually(t, "the second slot", func() bool { cs, _ := r.inbox.List(testProject); return len(cs) == 2 })
	if n := strings.Count(strings.Join(r.toasts(), "\n"), missing+" is not on this machine"); n != 1 {
		t.Fatalf("%d missing-root toasts today", n)
	}
	r.attach(t, "security")
	eventually(t, "claimed into the pane", func() bool { return len(r.q.Pending(pane)) == 2 })
	if c := r.q.Pending(pane)[0]; c.Status != queue.StatusPending {
		t.Fatalf("%+v", c)
	}
}

// A scheduled run on an attached pane counts toward that pane's session
// budgets: with the dot's call budget spent the slot neither runs nor is
// claimed (a later catch-up may still run it).
func TestScheduledRunRespectsTheSessionBudget(t *testing.T) {
	d := scheduledDot("Security", "0 11 * * *")
	d.Budget.MaxCallsPerSession = 1
	r := newProRig(t, proOpts{cwd: t.TempDir()}, d)
	r.attach(t, "security")
	eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	_ = r.usage.Add(pane, "security", state.Delta{Calls: 1})
	r.clock.Add(time.Hour + 20*time.Second)
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 0 {
		t.Fatalf("%d scheduled runs over the session budget", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.IsZero() {
		t.Fatalf("slot claimed (%v) although it did not run", last)
	}
}

func TestReloadDotsReplansTheNextSlot(t *testing.T) {
	r := newProRig(t, proOpts{cwd: t.TempDir()}, scheduledDot("Security", "0 11 * * *"))
	r.attach(t, "security")
	eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	r.setDots(scheduledDot("Security", "30 12 * * *"))
	eventually(t, "re-planned", func() bool { return r.dotState("security").Next == "scheduled Wed 12:30" })
	r.setDots(proDot("Security"))
	eventually(t, "no schedule, no hint", func() bool { return r.dotState("security").Next == "" })
}

func newYork(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// Review Focus 4: a slot inside the skipped DST hour does not exist and does
// not run; the schedule goes on at the next day's slot.
func TestScheduleSkipsASlotInTheSkippedDSTHour(t *testing.T) {
	ny := newYork(t)
	clk := &proClock{t: time.Date(2026, 3, 8, 1, 0, 0, 0, ny)} // clocks jump 02:00 EST -> 03:00 EDT
	r := newProRig(t, proOpts{cwd: t.TempDir(), clock: clk}, scheduledDot("Security", "30 2 * * *"))
	r.attach(t, "security")
	eventually(t, "planned past the skipped hour", func() bool { return r.dotState("security").Next == "scheduled Mon 02:30" })
	clk.Add(2 * time.Hour) // 01:00 EST -> 04:00 EDT: 02:30 never happened today
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 0 {
		t.Fatalf("%d runs for a slot in the skipped hour", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.IsZero() {
		t.Fatalf("claimed %v", last)
	}
	clk.Add(22*time.Hour + 30*time.Minute + 20*time.Second) // Mon 02:30:20 EDT
	eventually(t, "the next day's slot", func() bool { return len(scheduledRuns(r)) == 1 })
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.Equal(time.Date(2026, 3, 9, 2, 30, 0, 0, ny)) {
		t.Fatalf("claimed %v", last)
	}
}

// Review Focus 4: a slot inside the repeated DST hour runs once, at its
// first occurrence.
func TestScheduleRunsASlotInTheRepeatedDSTHourOnce(t *testing.T) {
	ny := newYork(t)
	clk := &proClock{t: time.Date(2026, 11, 1, 0, 50, 0, 0, ny)} // clocks fall back 02:00 EDT -> 01:00 EST
	r := newProRig(t, proOpts{cwd: t.TempDir(), clock: clk}, scheduledDot("Security", "30 1 * * *"))
	r.attach(t, "security")
	eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Sun 01:30" })
	first := time.Date(2026, 11, 1, 1, 30, 0, 0, ny) // time.Date picks the first (EDT) occurrence
	clk.Add(40*time.Minute + 20*time.Second)         // 01:30:20 EDT
	eventually(t, "the first occurrence runs", func() bool { return len(scheduledRuns(r)) == 1 })
	clk.Add(time.Hour) // 01:30:20 EST: the same wall-clock slot again
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 1 {
		t.Fatalf("%d runs for one slot in the repeated hour", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.Equal(first) {
		t.Fatalf("claimed %v, want %v", last, first)
	}
	eventually(t, "the next day's slot", func() bool { return r.dotState("security").Next == "scheduled Mon 01:30" })
}

// A slot is claimed only once the run passed every gate: a scheduled job that
// waits behind another background run, while the daily budget is spent, is
// neither run nor claimed.
func TestScheduledSlotIsClaimedOnlyWhenItRuns(t *testing.T) {
	r := newProRig(t, proOpts{cwd: t.TempDir()}, scheduledDot("Security", "0 11 * * *"))
	r.attach(t, "security")
	eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	r.e.bgSem <- struct{}{} // another background run holds the engine's slot
	r.clock.Add(time.Hour + 20*time.Second)
	eventually(t, "the scheduled job waits", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return r.e.bgQueued[key{pane, "security"}] > 0
	})
	if err := r.daily.Add("other", state.RunResearch, 5, r.clock.Now()); err != nil { // spent meanwhile
		t.Fatal(err)
	}
	<-r.e.bgSem
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 0 {
		t.Fatalf("%d scheduled runs over the daily budget", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.IsZero() {
		t.Fatalf("slot claimed (%v) although it did not run", last)
	}
}

// The standing task reaches the prompt only through PromptTask (sanitized and
// capped), never raw.
func TestScheduledQuestionUsesThePromptTask(t *testing.T) {
	d := scheduledDot("Security", "0 11 * * *")
	d.Schedule.Task = "Review ‮commits‬ for security issues."
	r := newProRig(t, proOpts{cwd: t.TempDir()}, d)
	r.attach(t, "security")
	eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	r.clock.Add(time.Hour + 20*time.Second)
	eventually(t, "the scheduled run", func() bool { return len(scheduledRuns(r)) == 1 })
	q := scheduledRuns(r)[0].Question
	if strings.ContainsAny(q, "‮‬") || !strings.HasPrefix(q, d.Schedule.PromptTask()+"\n") {
		t.Fatalf("question %q", q)
	}
}

// A project recorded by SeeDots alone has no name and no roots: it is skipped
// with one toast naming its id.
func TestAttachedProjectWithNoRootsIsSkipped(t *testing.T) {
	seen := func(m *memory.Store) { _ = m.SeeDots(testProject, []string{"security"}, at(8, 0)) }
	r := newProRig(t, proOpts{prepare: seen}, scheduledDot("Security", "0 11 * * *"))
	time.Sleep(60 * time.Millisecond)
	r.clock.Add(time.Hour + 20*time.Second)
	eventually(t, "a missing-root toast", func() bool {
		return strings.Contains(strings.Join(r.toasts(), "\n"), testProject+" is not on this machine")
	})
	time.Sleep(80 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 0 {
		t.Fatalf("%d runs on a project with no checkout", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.IsZero() {
		t.Fatalf("claimed %v", last)
	}
}

// A slot that comes while the dot's previous slot is still running waits for
// it (never two runs of one dot queued), then runs once: no burst, no loss.
func TestASlotWaitsForTheDotsRunningSlot(t *testing.T) {
	r := newProRig(t, proOpts{cwd: t.TempDir()}, scheduledDot("Security", "*/15 * * * *"))
	r.attach(t, "security")
	eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Wed 10:15" })
	gate := r.be.hold(backend.PassReview, false)
	r.clock.Add(15*time.Minute + 20*time.Second)
	eventually(t, "10:15 runs", func() bool { return len(scheduledRuns(r)) == 1 })
	r.clock.Add(15 * time.Minute) // 10:30:20 while 10:15 still runs
	time.Sleep(150 * time.Millisecond)
	r.e.mu.Lock()
	queued := r.e.bgQueued[key{pane, "security"}]
	r.e.mu.Unlock()
	if n := len(scheduledRuns(r)); n != 1 || queued != 1 {
		close(gate)
		t.Fatalf("%d runs, %d jobs queued while the dot's slot was still running", n, queued)
	}
	close(gate)
	eventually(t, "10:30 runs after", func() bool { return len(scheduledRuns(r)) == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 2 {
		t.Fatalf("%d runs", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.Equal(at(10, 30)) {
		t.Fatalf("claimed %v", last)
	}
}

// Carry-over from Task 6: a slot after now plus a small skew (a clock stepped
// back between the tick and the fire) is never claimed, since one future claim
// would refuse every earlier slot on that project.
func TestAFarFutureSlotIsNeverClaimed(t *testing.T) {
	d := scheduledDot("Security", "0 11 * * *")
	r := newProRig(t, proOpts{prepare: knownProject(t.TempDir(), "security", at(8, 0))}, d)
	eventually(t, "planned at 10:00", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return r.e.plan["security"].Equal(at(11, 0))
	})
	future := at(11, 0).AddDate(0, 0, 1)
	r.e.fireSchedule(dueSlot{dot: d, slot: future})
	r.e.fireSchedule(dueSlot{dot: d, slot: future, catchUp: true})
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.IsZero() {
		t.Fatalf("claimed a future slot %v", last)
	}
	if n := len(scheduledRuns(r)); n != 0 {
		t.Fatalf("%d runs for a future slot", n)
	}
	r.clock.Add(time.Hour + 20*time.Second)
	eventually(t, "today's slot still runs", func() bool { return len(scheduledRuns(r)) == 1 })
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.Equal(at(11, 0)) {
		t.Fatalf("claimed %v", last)
	}
}

// claimSlot never claims a slot after now plus claimSkew.
func TestClaimSlotRefusesASlotPastTheSkew(t *testing.T) {
	r := newProRig(t, proOpts{prepare: knownProject(t.TempDir(), "security", at(8, 0))}, proDot("Security"))
	if ok, err := r.e.claimSlot(testProject, "security", at(10, 0).Add(claimSkew+time.Minute)); ok || err == nil {
		t.Fatalf("claimed a slot past the skew: %v %v", ok, err)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.IsZero() {
		t.Fatalf("claimed %v", last)
	}
	if ok, err := r.e.claimSlot(testProject, "security", at(10, 0).Add(claimSkew)); !ok || err != nil {
		t.Fatalf("a slot within the skew: %v %v", ok, err)
	}
}

// Ruling P8-R12: "attached" expands to at most 10 targets, explicit paths
// first, then the projects the dot was attached to most recently; the
// dropped ones are logged once a day.
func TestScheduleTargetsAreCappedAtTen(t *testing.T) {
	base := t.TempDir()
	idOf := func(i int) string { return fmt.Sprintf("g-%016x", i+1) }
	projects := func(m *memory.Store) {
		for i := range 15 {
			root := filepath.Join(base, fmt.Sprint(i))
			_ = os.Mkdir(root, 0o700)
			_ = m.SeeProject(memory.Project{ID: idOf(i), Name: fmt.Sprint("repo", i), Root: root})
			_ = m.SeeDots(idOf(i), []string{"security"}, at(8, 0).Add(time.Duration(i)*time.Minute)) // later i = more recent
		}
	}
	logs := &logSink{}
	r := newProRig(t, proOpts{prepare: projects, deps: logs.deps}, proDot("Security"))
	d := scheduledDot("Security", "0 11 * * *")
	ids := func(ts []schedTarget) []string {
		var out []string
		for _, x := range ts {
			out = append(out, x.id)
		}
		return out
	}
	var want []string
	for i := 14; i >= 5; i-- {
		want = append(want, idOf(i))
	}
	if got := ids(r.e.scheduleTargets(d)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("attached targets %v, want the 10 most recent %v", got, want)
	}
	explicit := t.TempDir()
	d.Schedule.Projects = []string{dots.ScheduleAttached, explicit}
	got := ids(r.e.scheduleTargets(d))
	if len(got) != 10 || got[0] != testProject || strings.Join(got[1:], ",") != strings.Join(want[:9], ",") {
		t.Fatalf("targets %v: want the explicit path first, then the 9 most recent", got)
	}
	if n := logs.count("more than 10 projects"); n != 1 {
		t.Fatalf("%d dropped-project log lines, want one a day", n)
	}
}

// Ruling P8-R12: at most maxScheduledPerDay scheduled runs per dot per local
// day, counted in daily.json (every session). A slot over the cap is left
// unclaimed; the next day runs.
func TestScheduledRunsAreCappedPerDay(t *testing.T) {
	r := newProRig(t, proOpts{cwd: t.TempDir()}, scheduledDot("Security", "0 11 * * *"))
	r.attach(t, "security")
	eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	for range maxScheduledPerDay {
		if err := r.daily.Add("security", state.RunScheduled, 0, r.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	r.clock.Add(time.Hour + 20*time.Second)
	time.Sleep(150 * time.Millisecond)
	if n := len(scheduledRuns(r)); n != 0 {
		t.Fatalf("%d runs over the daily cap", n)
	}
	if last, _ := r.mem.LastSchedule(testProject, "security"); !last.IsZero() {
		t.Fatalf("slot claimed (%v) although the cap refused it", last)
	}
	r.clock.Add(24 * time.Hour)
	eventually(t, "the next day's slot", func() bool { return len(scheduledRuns(r)) == 1 })
}

// Ruling P8-R12: a stored claim days in the future (a skewed clock) does not
// block the project: the slot runs once and overwrites it, even with two
// engines racing for it.
func TestAFutureClaimIsOverwrittenAndTheSlotRunsOnce(t *testing.T) {
	shared, root := t.TempDir(), t.TempDir()
	clk := &proClock{t: at(10, 59)}
	future := at(11, 0).AddDate(0, 0, 3)
	seed := func(m *memory.Store) { _, _ = m.ClaimSchedule(testProject, "security", future, future) }
	d := scheduledDot("Security", "0 11 * * *")
	a := newProRig(t, proOpts{shared: shared, clock: clk, cwd: root, prepare: seed}, d)
	b := newProRig(t, proOpts{shared: shared, clock: clk, cwd: root}, d)
	a.attach(t, "security")
	b.attach(t, "security")
	time.Sleep(60 * time.Millisecond)
	a.e.bgSem <- struct{}{}
	b.e.bgSem <- struct{}{}
	clk.Add(90 * time.Second)
	eventually(t, "both engines queue the slot", func() bool { return queuedFor(a) > 0 && queuedFor(b) > 0 })
	<-a.e.bgSem
	<-b.e.bgSem
	eventually(t, "one run", func() bool { return len(scheduledRuns(a))+len(scheduledRuns(b)) == 1 })
	time.Sleep(150 * time.Millisecond)
	runs := append(scheduledRuns(a), scheduledRuns(b)...)
	if len(runs) != 1 {
		t.Fatalf("%d runs across two engines", len(runs))
	}
	if !strings.Contains(runs[0].Question, "Since: unknown") {
		t.Fatalf("question %q", runs[0].Question)
	}
	if last, _ := a.mem.LastSchedule(testProject, "security"); !last.Equal(at(11, 0)) {
		t.Fatalf("claimed %v, want the slot that ran", last)
	}
}

// Ruling P8-R12: when the proactive switch or memory comes back on, a stale
// plan re-plans from now: the slot that passed while it was off doesn't run.
func TestScheduleReplansWhenSwitchedBackOn(t *testing.T) {
	for name, set := range map[string]func(c *config.Config, on bool){
		"proactive": func(c *config.Config, on bool) { c.ProactiveEnabled = on },
		"memory":    func(c *config.Config, on bool) { c.MemoryEnabled = on },
	} {
		t.Run(name, func(t *testing.T) {
			r := newProRig(t, proOpts{cwd: t.TempDir()}, scheduledDot("Security", "0 11 * * *"))
			r.attach(t, "security")
			eventually(t, "planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
			toggle := func(on bool) {
				c := r.e.config()
				set(&c, on)
				r.e.cfg.Store(&c)
			}
			toggle(false)
			time.Sleep(60 * time.Millisecond)
			r.clock.Add(90 * time.Minute) // 11:30: the 11:00 slot passed while off
			time.Sleep(60 * time.Millisecond)
			toggle(true)
			eventually(t, "re-planned from now", func() bool { return r.dotState("security").Next == "scheduled Thu 11:00" })
			time.Sleep(100 * time.Millisecond)
			if n := len(scheduledRuns(r)); n != 0 {
				t.Fatalf("%d retroactive runs after switching back on", n)
			}
			r.clock.Add(23*time.Hour + 30*time.Minute + 20*time.Second)
			eventually(t, "the next slot", func() bool { return len(scheduledRuns(r)) == 1 })
		})
	}
}
