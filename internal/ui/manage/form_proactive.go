package manage

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/schedule"
	"herdr-dots/internal/textutil"
)

// Proactive fields (Phase 8). They come after every field earlier phases
// define and are found by label, so they never depend on field indexes.
const (
	lResearch = "Research while idle"
	lIdleMin  = "  idle minutes"
	lRuns     = "  research runs/day"
	lWeb      = "  research the web"
	lTopics   = "  research topics"
	lCron     = "Schedule (cron)"
	lTask     = "  scheduled task"
	lProjects = "  schedule projects"
	lCatchUp  = "  catch-up hours"
	lOnCommit = "Review each commit"
)

// previewSlots is how many upcoming cron slots the preview lists.
const previewSlots = 3

func proactiveFields(d dots.Dot) []field {
	task := textField(lTask, d.Schedule.Task)
	task.input.CharLimit = 2000
	topics := listField(lTopics, d.Research.Topics)
	topics.input.CharLimit = 2000
	projects := listField(lProjects, d.Schedule.Projects)
	projects.input.CharLimit = 2000
	return []field{
		{label: lResearch, kind: kBool, on: d.Research.Enabled},
		intField(lIdleMin, d.Research.IdleMinutes),
		intField(lRuns, d.Research.MaxRunsPerDay),
		{label: lWeb, kind: kBool, on: d.Research.Web},
		topics,
		textField(lCron, d.Schedule.Cron),
		task,
		projects,
		intField(lCatchUp, d.Schedule.CatchUpHours),
		{label: lOnCommit, kind: kBool, on: d.Trigger.OnCommit},
	}
}

// text and isOn read a field by label; a form without it reads as empty/off.
func (f *Form) text(label string) string {
	if fl := f.byLabel(label); fl != nil {
		return strings.TrimSpace(fl.input.Value())
	}
	return ""
}

func (f *Form) isOn(label string) bool {
	fl := f.byLabel(label)
	return fl != nil && fl.on
}

// byLabel returns the field with this label (nil if the form has none).
func (f *Form) byLabel(label string) *field {
	for i := range f.fields {
		if f.fields[i].label == label {
			return &f.fields[i]
		}
	}
	return nil
}

func (f *Form) labelNum(label string) (int, error) {
	fl := f.byLabel(label)
	if fl == nil {
		return 0, errNotNumber(strings.TrimSpace(label))
	}
	n, err := strconv.Atoi(strings.TrimSpace(fl.input.Value()))
	if err != nil {
		return 0, errNotNumber(strings.TrimSpace(label))
	}
	return n, nil
}

type errNotNumber string

func (e errNotNumber) Error() string { return string(e) + " must be a whole number" }

// applyProactive copies the proactive fields into d. dots.Validate checks
// the values (an invalid cron blocks the save with its message).
func (f *Form) applyProactive(d *dots.Dot) error {
	d.Research.Enabled = f.isOn(lResearch)
	d.Research.Web = f.isOn(lWeb)
	d.Research.Topics = splitList(f.text(lTopics))
	d.Schedule.Cron = f.text(lCron)
	d.Schedule.Task = f.text(lTask)
	d.Schedule.Projects = splitList(f.text(lProjects))
	d.Trigger.OnCommit = f.isOn(lOnCommit)
	var err error
	if d.Research.IdleMinutes, err = f.labelNum(lIdleMin); err != nil {
		return err
	}
	if d.Research.MaxRunsPerDay, err = f.labelNum(lRuns); err != nil {
		return err
	}
	if d.Schedule.CatchUpHours, err = f.labelNum(lCatchUp); err != nil {
		return err
	}
	return nil
}

func (f *Form) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// cleanLine makes text safe to show on one terminal line: no control
// characters (escape sequences); tabs and newlines become spaces.
func cleanLine(s string) string {
	s = strings.NewReplacer("\n", " ", "\t", " ", "\r", " ").Replace(s)
	return textutil.SanitizeModelText(s)
}

// errLabels maps the start of a dots.Validate message to the field it is
// about, so the error shows next to that field.
var errLabels = []struct{ prefix, label string }{
	{"research idle_minutes", lIdleMin},
	{"research max_runs_per_day", lRuns},
	{"research topic", lTopics},
	{"schedule cron", lCron},
	{"schedule task", lTask},
	{"schedule project", lProjects},
	{"schedule catch_up_hours", lCatchUp},
}

// refreshProblems recomputes the per-field validation messages from the
// current values; View calls it once per render.
func (f *Form) refreshProblems() {
	f.problems = map[string]string{}
	add := func(label, msg string) {
		msg = cleanLine(msg)
		if prev := f.problems[label]; prev != "" {
			msg = prev + "; " + msg
		}
		f.problems[label] = msg
	}
	d := f.orig
	if err := f.applyProactive(&d); err != nil {
		for _, l := range []string{lIdleMin, lRuns, lCatchUp} {
			if _, e := f.labelNum(l); e != nil {
				add(l, e.Error())
			}
		}
	}
	verr := d.Validate()
	if verr == nil {
		return
	}
	var joined interface{ Unwrap() []error }
	list := []error{verr}
	if errors.As(verr, &joined) {
		list = joined.Unwrap()
	}
	for _, e := range list {
		msg := e.Error()
		for _, m := range errLabels {
			if strings.HasPrefix(msg, m.prefix) {
				add(m.label, msg)
				break
			}
		}
	}
}

// proactiveNote is the text shown under a field (lines split by "\n"; a line
// starting "  ! " is an error): validation errors first, then the cron
// preview, the read-only labels and the web notes. "" = none.
func (f *Form) proactiveNote(label string) string {
	if prob := f.problems[label]; prob != "" {
		note := "  ! " + prob // an invalid or too-frequent cron gets no preview
		if label == lCron {
			note += "\n  scheduled runs are read-only"
		}
		return note
	}
	switch label {
	case lResearch:
		return "  read-only: researches with read-only repo tools, never edits files"
	case lCron:
		expr := f.text(lCron)
		if expr == "" {
			return "  off (5 fields: minute hour day month weekday, local time; runs while Herdr runs)\n  scheduled runs are read-only"
		}
		spec, err := schedule.Parse(expr)
		if err != nil {
			return "  ! " + cleanLine(err.Error())
		}
		var slots []string
		at := f.clock()
		for i := 0; i < previewSlots; i++ {
			if at = spec.Next(at); at.IsZero() {
				break
			}
			slots = append(slots, at.Local().Format("Mon 02 Jan 15:04"))
		}
		if len(slots) == 0 {
			return "  never fires"
		}
		return "  next: " + strings.Join(slots, " · ") + "\n  local time; scheduled runs are read-only"
	case lWeb:
		if !f.isOn(lWeb) {
			return ""
		}
		if !backend.WebSupported(f.Kind()) {
			return "  " + f.Kind() + " has no web tools: this dot researches without the web"
		}
		return "  search queries derived from your code go to the provider's web search"
	}
	return ""
}
