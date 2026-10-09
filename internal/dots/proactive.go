package dots

import (
	"fmt"
	"path/filepath"
	"strings"
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
	return d.Research.Enabled || d.Schedule.Cron != "" || d.Trigger.OnCommit
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
		if strings.TrimSpace(t) == "" || utf8.RuneCountInString(t) > 200 {
			errs = append(errs, fmt.Errorf("research topic %q must be 1-200 characters", t))
		}
	}
	return errs
}

func (s ScheduleSpec) validate() []error {
	var errs []error
	if s.CatchUpHours < 0 || s.CatchUpHours > 168 {
		errs = append(errs, fmt.Errorf("schedule catch_up_hours must be 0-168, got %d", s.CatchUpHours))
	}
	if strings.TrimSpace(s.Cron) == "" {
		return errs
	}
	if _, err := schedule.Parse(s.Cron); err != nil {
		errs = append(errs, fmt.Errorf("schedule cron: %v", err))
	}
	if strings.TrimSpace(s.Task) == "" {
		errs = append(errs, fmt.Errorf("schedule task is required when cron is set"))
	} else if utf8.RuneCountInString(s.Task) > 2000 {
		errs = append(errs, fmt.Errorf("schedule task must be at most 2000 characters"))
	}
	for _, p := range s.Projects {
		if p != ScheduleAttached && !filepath.IsAbs(p) {
			errs = append(errs, fmt.Errorf("schedule project %q must be an absolute path or %q", p, ScheduleAttached))
		}
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
