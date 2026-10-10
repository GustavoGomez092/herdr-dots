package engine

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/schedule"
	"herdr-dots/internal/state"
)

// Scheduled dots (Phase 8, spec §6.2). Schedules run only while Herdr runs:
// there is no OS scheduler (ruling 10). poll checks every scheduled dot's
// next slot. A slot that came is claimed per project in that project's
// memory (schedule.json, under the memory lock), so engines in two Herdr
// sessions run it once. After a gap (engine start, a sleeping laptop) only
// the latest missed slot runs, and only within catch_up_hours. Schedules
// need memory: claims and project roots live there.
//
// Slots come from the injected clock (e.now()), in its location, and are
// claimed as UTC instants: a slot's identity is its instant, so two processes
// can't name one slot two ways, and a repeated DST hour (schedule.Spec returns
// its first occurrence) is one slot.

// attachedWindow: projects = ["attached"] means projects this dot was
// attached to within this window.
const attachedWindow = 14 * 24 * time.Hour

// lateGrace is how late a slot may be noticed and still run when
// catch_up_hours is 0 (poll runs every few seconds).
const lateGrace = 2 * time.Minute

// claimSkew: a slot later than now plus this is never claimed. ClaimSchedule
// refuses a slot at or before the last claim, so one future claim (a clock
// stepped back between the tick and the claim) would block every earlier slot.
const claimSkew = memory.ScheduleSkew

// maxScheduledPerDay caps a dot's scheduled runs per local day across every
// Herdr session (daily.json; ruling P8-R12). Every project's run counts, so
// a 15-minute cron (the shortest allowed) fills it on one project, and on
// several projects it fills sooner.
const maxScheduledPerDay = 96

// schedPane keys a scheduled dot's fire in bgQueued (no Herdr pane id looks
// like it): scheduleTick reserves it, the fire releases it, and a slot that
// comes meanwhile waits for it instead of queueing a second run of the dot.
const schedPane = "(schedule)"

type schedTarget struct{ id, name, cwd string }

type dueSlot struct {
	dot     dots.Dot
	slot    time.Time
	catchUp bool // the slot passed before this engine planned the dot
}

func catchUpWindow(d dots.Dot) time.Duration {
	return time.Duration(d.Schedule.CatchUpHours) * time.Hour
}

// scheduleTick fires every slot that came. Called from poll; it only does
// in-memory work under e.mu and spawns the fires.
func (e *Engine) scheduleTick() {
	if !e.proactiveOn() || !e.memEnabled() {
		e.mu.Lock()
		e.schedOff = true // back on, every schedule re-plans from then
		e.mu.Unlock()
		return
	}
	now := e.now()
	var due []dueSlot
	var late []string
	changed := false
	e.mu.Lock()
	resume := e.schedOff
	e.schedOff = false
	for slug, d := range e.dots {
		spec, err := schedule.Parse(d.Schedule.Cron)
		if strings.TrimSpace(d.Schedule.Cron) == "" || err != nil {
			if _, ok := e.plan[slug]; ok {
				delete(e.plan, slug)
				changed = true
			}
			continue
		}
		if resume {
			// The proactive switch or memory came back on: slots that passed
			// while it was off never run (no retroactive run, no catch-up).
			e.plan[slug], changed = spec.Next(now), true
		}
		k := key{schedPane, slug}
		if e.bgQueued[k] > 0 {
			continue // the dot's last slot is still running: a slot that came meanwhile runs after it
		}
		next, planned := e.plan[slug]
		catchUp := false
		if !planned {
			next = spec.Next(now)
			if prev := spec.Prev(now.Add(time.Nanosecond)); !prev.IsZero() {
				if now.Sub(prev) <= catchUpWindow(d) {
					next, catchUp = prev, true // missed while Herdr was closed: catch up once
				} else {
					late = append(late, fmt.Sprintf("dot %s: last slot %s is older than catch_up_hours (%d h); not caught up", slug, prev.Format(time.RFC3339), d.Schedule.CatchUpHours))
				}
			}
			e.plan[slug], changed = next, true
		}
		if next.IsZero() || now.Before(next) {
			continue
		}
		slot := spec.Prev(now.Add(time.Nanosecond)) // the latest slot that came: one run after any gap
		e.plan[slug], changed = spec.Next(now), true
		if slot.IsZero() || now.Sub(slot) > max(catchUpWindow(d), lateGrace) {
			late = append(late, fmt.Sprintf("dot %s: slot %s missed by more than %d h; skipped", slug, slot.Format(time.RFC3339), d.Schedule.CatchUpHours))
			continue
		}
		e.bgQueued[k]++ // reserved with the check above; released when the fire ends
		due = append(due, dueSlot{dot: d, slot: slot, catchUp: catchUp})
	}
	e.mu.Unlock()
	for _, l := range late {
		e.d.Logf("%s", l)
	}
	if changed {
		e.broadcast() // the roster's "scheduled Mon 09:00"
	}
	for _, x := range due {
		if !e.spawn(func() {
			defer e.fireDone(x.dot.Slug)
			e.fireSchedule(x)
		}) {
			e.fireDone(x.dot.Slug) // the engine is shutting down: release the reservation
		}
	}
}

// fireDone releases a fire's reservation (scheduleTick) on every path out.
func (e *Engine) fireDone(slug string) {
	k := key{schedPane, slug}
	e.mu.Lock()
	if e.bgQueued[k]--; e.bgQueued[k] <= 0 {
		delete(e.bgQueued, k)
	}
	e.mu.Unlock()
}

// fireSchedule runs one slot of a dot on each of its projects, one project
// at a time. Off every engine lock.
func (e *Engine) fireSchedule(x dueSlot) {
	d := x.dot
	if x.slot.After(e.now().Add(claimSkew)) {
		e.d.Logf("dot %s: slot %s is in the future (the clock changed?); skipped", d.Slug, x.slot.Format(time.RFC3339))
		return
	}
	label := "scheduled " + x.slot.Format("Mon 15:04")
	for _, t := range e.scheduleTargets(d) {
		var last time.Time
		if err := retryLocked(func() error {
			var err error
			last, err = e.d.Memory.LastSchedule(t.id, d.Slug)
			return err
		}); err != nil {
			e.d.Logf("dot %s: schedule on %s: %v", d.Slug, t.name, err)
			continue
		}
		// A claim after now plus claimSkew was written by a skewed clock: it is
		// treated as absent (ClaimSchedule overwrites it too, ruling P8-R12).
		future := last.After(e.now().Add(claimSkew))
		if future {
			if e.onceToday("log:future-claim:" + d.Slug + ":" + t.id) {
				e.d.Logf("dot %s: the last claim on %s (%s) is in the future (a clock was wrong); treated as absent", d.Slug, t.name, last.Format(time.RFC3339))
			}
		} else if !last.Before(x.slot) {
			continue // this engine or another session already ran it
		}
		if x.catchUp && last.IsZero() {
			// A schedule that never ran starts at its next slot: record this
			// one instead of running it retroactively.
			if _, err := e.claimSlot(t.id, d.Slug, x.slot); err != nil {
				e.d.Logf("dot %s: schedule on %s: %v", d.Slug, t.name, err)
			}
			continue
		}
		paneID, att, paused, gen := e.paneOnProject(t.id, d.Slug)
		if r := e.backgroundGate(d.Slug, state.RunScheduled, maxScheduledPerDay, paused); r != ReasonRun {
			// Once a day per project and reason: a capped 15-minute schedule
			// would otherwise log every slot.
			if e.onceToday("log:skip:" + d.Slug + ":" + t.id + ":" + string(r)) {
				e.d.Logf("dot %s: %s on %s skipped (%s; logged once today)", d.Slug, label, t.name, r)
			}
			continue // not claimed: a later catch-up may still run it
		}
		if paneID != "" {
			// A run on a pane counts toward its session budgets (runBackground
			// checks again after the wait, before it claims the slot).
			e.mu.Lock()
			over := e.bgOverBudgetLocked(bgJob{paneID: paneID, dot: d, gen: gen})
			e.mu.Unlock()
			if over {
				e.d.Logf("dot %s: %s on %s skipped (%s on %s)", d.Slug, label, t.name, ReasonSessionBudget, paneID)
				continue
			}
		}
		if !last.IsZero() {
			last = last.In(x.slot.Location())
		}
		j := bgJob{kind: state.RunScheduled, paneID: paneID, session: att.SessionValue, project: t.id, projectName: t.name,
			cwd: t.cwd, dot: d, question: scheduledTask(d, last, future), label: label, maxPerDay: maxScheduledPerDay,
			paused: paused, gen: gen, deliver: e.deliverScheduled}
		j.claim = func() bool {
			ok, err := e.claimSlot(t.id, d.Slug, x.slot)
			switch {
			case err != nil:
				e.d.Logf("dot %s: schedule on %s: %v", d.Slug, t.name, err)
			case !ok:
				e.d.Logf("dot %s: %s on %s already ran in another session", d.Slug, label, t.name)
			}
			return ok
		}
		if paneID != "" {
			turns, err := e.reader(att).Turns(e.ctx)
			if err != nil {
				e.d.Logf("pane %s: transcript read for %s: %v", paneID, label, err)
			}
			j.turns = tail(turns, d.Context.Turns)
			if len(j.turns) > 0 {
				j.turn = j.turns[len(j.turns)-1].Index
			}
		}
		e.runBackground(j)
	}
}

// claimSlot claims slot for a dot on project pid across every Herdr session
// (memory.ClaimSchedule, as a UTC instant). It never claims a slot after now
// plus claimSkew. Off every engine lock.
func (e *Engine) claimSlot(pid, slug string, slot time.Time) (bool, error) {
	if slot.After(e.now().Add(claimSkew)) {
		return false, fmt.Errorf("slot %s is in the future (the clock changed?); not claimed", slot.Format(time.RFC3339))
	}
	var ok bool
	err := retryLocked(func() error {
		var err error
		ok, err = e.d.Memory.ClaimSchedule(pid, slug, slot.UTC(), e.now())
		return err
	})
	return ok, err
}

// scheduledTask is a scheduled run's Question: the standing task (sanitized
// and capped: PromptTask, never the raw Task) and the last slot it ran for on
// this project (unknown when the stored claim was in the future).
func scheduledTask(d dots.Dot, last time.Time, unknown bool) string {
	since := "never (this is the first scheduled run on this project)"
	switch {
	case unknown:
		since = "unknown (the last recorded run had a wrong clock)"
	case !last.IsZero():
		since = last.Format("Mon 2 Jan 2006 15:04 MST")
	}
	return d.Schedule.PromptTask() + "\nSince: " + since
}

// maxScheduleTargets caps the projects one slot runs on (ruling P8-R5/P8-R12).
const maxScheduleTargets = 10

// scheduleTargets resolves a dot's [schedule] projects to project ids and
// checkouts on this machine, at most maxScheduleTargets: explicit paths
// first, then "attached" projects, the most recently attached first. The rest
// are logged once a day. Missing roots are skipped with a daily toast.
func (e *Engine) scheduleTargets(d dots.Dot) []schedTarget {
	if d.Origin == dots.OriginProject {
		// A project dot runs on its own checkout only (ruling P9-R13).
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.projectTargetLocked(d)
	}
	var out []schedTarget
	var dropped []string
	seen := map[string]bool{}
	full := func(name string) bool {
		if len(out) < maxScheduleTargets {
			return false
		}
		dropped = append(dropped, name)
		return true
	}
	add := func(t schedTarget) {
		if !seen[t.id] && !full(t.name) {
			seen[t.id] = true
			out = append(out, t)
		}
	}
	attached := false
	for _, p := range d.Schedule.Targets() {
		if p == dots.ScheduleAttached {
			attached = true
			continue
		}
		if full(p) {
			continue
		}
		if !isDir(p) {
			e.rootMissing(d, p)
			continue
		}
		pr := e.resolve(p)
		if pr.ID == "" {
			e.d.Logf("dot %s: scheduled run skipped: %s is not a project memory can track", d.Slug, p)
			continue
		}
		name := pr.Name
		if name == "" {
			name = p
		}
		add(schedTarget{id: pr.ID, name: name, cwd: p})
	}
	if attached {
		metas, err := e.d.Memory.RecentDotProjects(d.Slug, e.now().Add(-attachedWindow))
		if err != nil {
			e.d.Logf("dot %s: attached projects: %v", d.Slug, err)
		}
		sort.SliceStable(metas, func(i, j int) bool { return metas[i].DotsSeen[d.Slug].After(metas[j].DotsSeen[d.Slug]) })
		for _, m := range metas {
			name := m.Name
			if name == "" { // recorded by SeeDots before SeeProject filled it in
				name = m.ID
			}
			if seen[m.ID] || full(name) {
				continue
			}
			root := ""
			for i := len(m.Roots) - 1; i >= 0 && root == ""; i-- { // the newest checkout first
				if isDir(m.Roots[i]) {
					root = m.Roots[i]
				}
			}
			if root == "" { // no checkout here, or no roots recorded yet
				e.rootMissing(d, name)
				continue
			}
			add(schedTarget{id: m.ID, name: name, cwd: root})
		}
	}
	if len(dropped) > 0 && e.onceToday("targets:"+d.Slug) {
		e.d.Logf("dot %s: schedule reaches more than %d projects; skipped %s", d.Slug, maxScheduleTargets, strings.Join(dropped, ", "))
	}
	return out
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// rootMissing logs a project a schedule can't reach and toasts once a day.
func (e *Engine) rootMissing(d dots.Dot, name string) {
	e.d.Logf("dot %s: scheduled run skipped: %s is not on this machine", d.Slug, name)
	if e.onceToday("root:" + d.Slug + ":" + name) {
		e.notify(d, "scheduled run skipped: "+name+" is not on this machine")
	}
}

// paneOnProject picks the pane a scheduled result goes to: one where the dot
// is attached, else any pane on the project, else none (the inbox). It also
// returns that attachment's generation: the list and the generation are read
// in one e.mu section (e.mu -> stores), so the job is tied to this attachment
// and a pane closed (or reused) while the run is out never gets its card.
func (e *Engine) paneOnProject(pid, slug string) (string, state.Attachment, bool, uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var other state.Attachment
	found := false
	for _, a := range e.d.Attachments.List() {
		if a.ProjectID != pid {
			continue
		}
		if da := a.Find(slug); da != nil {
			return a.PaneID, a, da.Paused, e.gens[a.PaneID]
		}
		if !found {
			other, found = a, true
		}
	}
	if found {
		return other.PaneID, other, false, e.gens[other.PaneID]
	}
	return "", state.Attachment{}, false, 0
}

// seeDots records which dots are attached on a project (for "attached").
func (e *Engine) seeDots(pid string, slugs []string) {
	if !e.memEnabled() || pid == "" || len(slugs) == 0 {
		return
	}
	if err := retryLocked(func() error { return e.d.Memory.SeeDots(pid, slugs, e.now()) }); err != nil {
		e.d.Logf("memory: record dots on %s: %v", pid, err)
	}
}

func slugsOf(a state.Attachment) []string {
	out := make([]string, 0, len(a.Dots))
	for _, da := range a.Dots {
		out = append(out, da.Slug)
	}
	return out
}

// scheduleNextLocked is the roster hint "scheduled Mon 09:00". e.mu held.
func (e *Engine) scheduleNextLocked(d dots.Dot, now time.Time) string {
	if next, ok := e.plan[d.Slug]; ok && strings.TrimSpace(d.Schedule.Cron) != "" && next.After(now) {
		return "scheduled " + next.Format("Mon 15:04")
	}
	return ""
}
