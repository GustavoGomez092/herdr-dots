package dots

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"herdr-dots/internal/schedule"
	"herdr-dots/internal/textutil"
)

// ScheduleAttached in [schedule] projects means "every project this dot was
// attached to in the last 14 days".
const ScheduleAttached = "attached"

// ResearchSpec is a dot's [research] table: read-only background research
// while the main agent sits idle. Off by default.
type ResearchSpec struct {
	Enabled       bool     `toml:"enabled"`
	IdleMinutes   int      `toml:"idle_minutes"`     // idle time (no new turns) before a pass
	MaxRunsPerDay int      `toml:"max_runs_per_day"` // across every Herdr session
	Web           bool     `toml:"web"`              // web search/fetch where the backend supports it
	Topics        []string `toml:"topics"`           // standing topics added to every research brief
}

// ScheduleSpec is a dot's [schedule] table: a standing task on a cron
// schedule, run while Herdr is running. An empty Cron means off.
type ScheduleSpec struct {
	Cron         string   `toml:"cron"`           // 5-field cron in local time; "" = off
	Task         string   `toml:"task"`           // what to do at each slot
	Projects     []string `toml:"projects"`       // absolute paths or "attached"; empty = ["attached"]
	CatchUpHours int      `toml:"catch_up_hours"` // a slot missed while Herdr was closed runs once at start if this recent
}

func DefaultResearch() ResearchSpec {
	return ResearchSpec{IdleMinutes: 10, MaxRunsPerDay: 6, Topics: []string{}}
}

func DefaultSchedule() ScheduleSpec {
	return ScheduleSpec{Projects: []string{}, CatchUpHours: 12}
}

// Targets is Projects with the default applied.
func (s ScheduleSpec) Targets() []string {
	if len(s.Projects) == 0 {
		return []string{ScheduleAttached}
	}
	return s.Projects
}

// Proactive reports whether the dot does any background work.
func (d Dot) Proactive() bool {
	return d.Research.Enabled || strings.TrimSpace(d.Schedule.Cron) != "" || d.Trigger.OnCommit
}

// unsafeText reports a control rune other than (when allowed) newline and tab.
func unsafeText(s string, allowWhitespace bool) bool {
	for _, r := range s {
		if !unicode.IsControl(r) {
			continue
		}
		if allowWhitespace && (r == '\n' || r == '\t') {
			continue
		}
		return true
	}
	return false
}

func (r ResearchSpec) validate() []error {
	var errs []error
	if r.IdleMinutes < 1 || r.IdleMinutes > 1440 {
		errs = append(errs, fmt.Errorf("research idle_minutes must be 1-1440, got %d", r.IdleMinutes))
	}
	if r.MaxRunsPerDay < 1 || r.MaxRunsPerDay > 100 {
		errs = append(errs, fmt.Errorf("research max_runs_per_day must be 1-100, got %d", r.MaxRunsPerDay))
	}
	if len(r.Topics) > 10 {
		errs = append(errs, fmt.Errorf("research topics: at most 10, got %d", len(r.Topics)))
	}
	for _, t := range r.Topics {
		if strings.TrimSpace(t) == "" || strings.TrimSpace(textutil.SanitizeModelText(t)) == "" || utf8.RuneCountInString(t) > 200 {
			errs = append(errs, fmt.Errorf("research topic %q must be 1-200 characters", t))
		} else if unsafeText(t, false) {
			errs = append(errs, fmt.Errorf("research topic %q must be one line with no control characters", t))
		}
	}
	return errs
}

// Minimum gap between two scheduled slots (controller ruling P8-R5).
const (
	minScheduleGap   = 15 * time.Minute
	scheduleProbe    = 7 * 24 * time.Hour
	scheduleMaxSlots = 7 * 24 * 60
)

// tooFrequent walks a week of slots from a fixed reference time (UTC; dots
// have no time zone setting) and reports whether any gap is under 15 minutes.
func tooFrequent(spec schedule.Spec) bool {
	ref := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	end := ref.Add(scheduleProbe)
	prev := spec.Next(ref.Add(-time.Second))
	for i := 0; i < scheduleMaxSlots && !prev.IsZero() && prev.Before(end); i++ {
		next := spec.Next(prev)
		if next.IsZero() {
			return false
		}
		if next.Sub(prev) < minScheduleGap {
			return true
		}
		prev = next
	}
	return false
}

func (s ScheduleSpec) validate() []error {
	var errs []error
	if s.CatchUpHours < 0 || s.CatchUpHours > 168 {
		errs = append(errs, fmt.Errorf("schedule catch_up_hours must be 0-168, got %d", s.CatchUpHours))
	}
	// Task and projects are checked even with no cron, so a hidden oversized
	// or unsafe value can't sit in the file.
	if utf8.RuneCountInString(s.Task) > 2000 {
		errs = append(errs, fmt.Errorf("schedule task must be at most 2000 characters"))
	}
	if unsafeText(s.Task, true) {
		errs = append(errs, fmt.Errorf("schedule task must not contain control characters"))
	}
	if len(s.Projects) > 10 {
		errs = append(errs, fmt.Errorf("schedule projects: at most 10, got %d", len(s.Projects)))
	}
	seen := map[string]bool{}
	for _, p := range s.Projects {
		if p != ScheduleAttached && !filepath.IsAbs(p) {
			errs = append(errs, fmt.Errorf("schedule project %q must be an absolute path or %q", p, ScheduleAttached))
			continue
		}
		key := p
		if p != ScheduleAttached {
			key = filepath.Clean(p)
		}
		if seen[key] {
			errs = append(errs, fmt.Errorf("schedule project %q is a duplicate", p))
		}
		seen[key] = true
	}
	if strings.TrimSpace(s.Cron) == "" {
		return errs
	}
	if spec, err := schedule.Parse(s.Cron); err != nil {
		errs = append(errs, fmt.Errorf("schedule cron: %v", err))
	} else if tooFrequent(spec) {
		errs = append(errs, fmt.Errorf("schedule cron fires more often than every 15 minutes"))
	}
	if strings.TrimSpace(s.Task) == "" {
		errs = append(errs, fmt.Errorf("schedule task is required when cron is set"))
	}
	return errs
}

// Text that reaches a prompt is sanitized and capped, whatever the file held.
const (
	maxTaskRunes  = 2000
	maxTopicRunes = 200
	maxTopics     = 10
)

func capRunes(s string, n int) string {
	s = strings.TrimSpace(textutil.SanitizeModelText(s))
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// PromptTask is the schedule task, sanitized and capped for a prompt.
func (s ScheduleSpec) PromptTask() string { return capRunes(s.Task, maxTaskRunes) }

// PromptTopics are the research topics, sanitized and capped for a prompt.
func (r ResearchSpec) PromptTopics() []string {
	out := []string{}
	for _, t := range r.Topics {
		if t = capRunes(t, maxTopicRunes); t != "" && len(out) < maxTopics {
			out = append(out, t)
		}
	}
	return out
}
