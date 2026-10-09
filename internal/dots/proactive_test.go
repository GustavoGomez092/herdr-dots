package dots

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func proactiveDot() Dot {
	d := New("Security")
	d.Purpose = "Find auth bugs."
	return d
}

func TestNewHasProactiveDefaultsOff(t *testing.T) {
	d := New("Security")
	if !reflect.DeepEqual(d.Research, ResearchSpec{IdleMinutes: 10, MaxRunsPerDay: 6, Topics: []string{}}) {
		t.Fatalf("research %+v", d.Research)
	}
	if !reflect.DeepEqual(d.Schedule, ScheduleSpec{Projects: []string{}, CatchUpHours: 12}) {
		t.Fatalf("schedule %+v", d.Schedule)
	}
	if d.Trigger.OnCommit || d.Proactive() {
		t.Fatal("research, schedules and commit triggers are off by default")
	}
	if got := d.Schedule.Targets(); len(got) != 1 || got[0] != ScheduleAttached {
		t.Fatalf("default targets %v", got)
	}
}

func TestTemplatesShipWithProactiveOff(t *testing.T) {
	for _, d := range Templates() {
		if d.Proactive() || d.Research.IdleMinutes != 10 || d.Schedule.CatchUpHours != 12 {
			t.Errorf("template %s: research %+v schedule %+v on_commit %v", d.Slug, d.Research, d.Schedule, d.Trigger.OnCommit)
		}
	}
}

func TestV1DotFileGetsProactiveDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "security.toml"), []byte(v1Dot), 0o600)
	d, err := Load(dir, "security")
	if err != nil || d.Proactive() || d.Research.IdleMinutes != 10 || d.Schedule.CatchUpHours != 12 {
		t.Fatalf("%+v %+v %v", d.Research, d.Schedule, err)
	}
}

func TestProactiveTablesLoadAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := v1Dot + `
[research]
enabled = true
idle_minutes = 20
web = true
topics = ["dependency advisories for packages in package.json"]

[schedule]
cron = "0 9 * * 1-5"
task = "Review commits since the last run for security issues."
projects = ["attached", 'API_DIR']
`
	api := filepath.Join(dir, "api") // absolute on every OS; a TOML literal string needs no escaping
	body = strings.Replace(body, "API_DIR", api, 1)
	body = strings.Replace(body, "cooldown = 0\n", "cooldown = 0\non_commit = true\n", 1)
	os.WriteFile(filepath.Join(dir, "security.toml"), []byte(body), 0o600)
	d, err := Load(dir, "security")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Research.Enabled || d.Research.IdleMinutes != 20 || d.Research.MaxRunsPerDay != 6 || !d.Research.Web || len(d.Research.Topics) != 1 {
		t.Fatalf("research %+v (partial tables keep the other defaults)", d.Research)
	}
	if d.Schedule.Cron != "0 9 * * 1-5" || d.Schedule.CatchUpHours != 12 || len(d.Schedule.Projects) != 2 || d.Schedule.Projects[1] != api || !d.Trigger.OnCommit {
		t.Fatalf("schedule %+v on_commit %v", d.Schedule, d.Trigger.OnCommit)
	}
	if err := Save(dir, d, false); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir, "security")
	if err != nil || !reflect.DeepEqual(again.Research, d.Research) || !reflect.DeepEqual(again.Schedule, d.Schedule) || !again.Trigger.OnCommit {
		t.Fatalf("round trip %+v %+v %v", again.Research, again.Schedule, err)
	}
}

func TestProactiveValidation(t *testing.T) {
	cases := map[string]func(*Dot){
		"research idle_minutes must be 1-1440":    func(d *Dot) { d.Research.IdleMinutes = 0 },
		"research max_runs_per_day must be 1-100": func(d *Dot) { d.Research.MaxRunsPerDay = 101 },
		"research topics: at most 10":             func(d *Dot) { d.Research.Topics = make([]string, 11) },
		"must be 1-200 characters":                func(d *Dot) { d.Research.Topics = []string{strings.Repeat("x", 201)} },
		"schedule cron":                           func(d *Dot) { d.Schedule.Cron, d.Schedule.Task = "61 * * * *", "x" },
		"schedule task is required":               func(d *Dot) { d.Schedule.Cron = "0 9 * * *" },
		"must be an absolute path or \"attached\"": func(d *Dot) {
			d.Schedule.Cron, d.Schedule.Task, d.Schedule.Projects = "0 9 * * *", "x", []string{"src/api"}
		},
		"schedule catch_up_hours must be 0-168": func(d *Dot) { d.Schedule.CatchUpHours = 169 },
		"schedule task must be at most 2000":    func(d *Dot) { d.Schedule.Cron, d.Schedule.Task = "@daily", strings.Repeat("x", 2001) },
	}
	for want, mutate := range cases {
		d := proactiveDot()
		mutate(&d)
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
	d := proactiveDot()
	d.Schedule.Task = "a task with no cron is kept but does nothing"
	if err := d.Validate(); err != nil {
		t.Fatalf("an empty cron means off: %v", err)
	}
}

// An invalid cron in a file is flagged in the library like any invalid dot.
func TestLoadAllFlagsInvalidCron(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "security.toml"), []byte(v1Dot+"\n[schedule]\ncron = \"every day\"\ntask = \"x\"\n"), 0o600)
	ok, bad := LoadAll(dir)
	if len(ok) != 0 || len(bad) != 1 || !strings.Contains(bad[0].Err.Error(), "schedule cron") {
		t.Fatalf("ok %v bad %v", ok, bad)
	}
}

func TestProactiveBoundsRejectZeroAndNegative(t *testing.T) {
	for _, mutate := range []func(*Dot){
		func(d *Dot) { d.Research.IdleMinutes = 0 },
		func(d *Dot) { d.Research.IdleMinutes = -5 },
		func(d *Dot) { d.Research.IdleMinutes = 1441 },
		func(d *Dot) { d.Research.MaxRunsPerDay = 0 },
		func(d *Dot) { d.Research.MaxRunsPerDay = -1 },
		func(d *Dot) { d.Schedule.CatchUpHours = -1 },
	} {
		d := proactiveDot()
		mutate(&d)
		if d.Validate() == nil {
			t.Errorf("%+v %+v should be invalid", d.Research, d.Schedule)
		}
	}
	d := proactiveDot()
	d.Schedule.CatchUpHours = 0
	if err := d.Validate(); err != nil {
		t.Fatalf("catch_up_hours 0 means no catch-up: %v", err)
	}
}

func TestPromptTextIsSanitizedAndCapped(t *testing.T) {
	s := ScheduleSpec{Task: "check\x1b]52;c;x\x07 it " + strings.Repeat("y", 3000)}
	got := s.PromptTask()
	if strings.ContainsAny(got, "\x1b\x07") || len([]rune(got)) != 2000 {
		t.Fatalf("task %q", got)
	}
	r := ResearchSpec{Topics: []string{"a\x1b[2Jb", "  ", strings.Repeat("z", 500)}}
	topics := r.PromptTopics()
	if len(topics) != 2 || topics[0] != "a[2Jb" || len([]rune(topics[1])) != 200 {
		t.Fatalf("topics %q", topics)
	}
}

func TestProactiveSettingsDoNotTouchCapabilities(t *testing.T) {
	d := proactiveDot()
	before := d.Capabilities
	d.Research.Enabled, d.Research.Web = true, true
	d.Schedule.Cron, d.Schedule.Task = "@daily", "x"
	d.Trigger.OnCommit = true
	if d.Capabilities != before || d.Capabilities.Write {
		t.Fatal("proactive settings must not grant write")
	}
}
