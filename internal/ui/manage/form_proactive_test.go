package manage

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
)

func typeInto(f *Form, label, text string, clear int) {
	focusOn(f, label)
	for i := 0; i < clear; i++ {
		f.Update(k("backspace"))
	}
	for _, r := range text {
		f.Update(k(string(r)))
	}
}

func TestFormEditsProactiveSettings(t *testing.T) {
	d, _ := dots.FromTemplate("security")
	f := NewForm(d, false)
	focusOn(f, lResearch)
	f.Update(k("space"))
	typeInto(f, lIdleMin, "20", 2)
	focusOn(f, lWeb)
	f.Update(k("space"))
	typeInto(f, lTopics, "advisories for package.json, auth flows", 0)
	typeInto(f, lCron, "0 9 * * 1-5", 0)
	typeInto(f, lTask, "Review commits since the last run.", 0)
	typeInto(f, lProjects, "attached", 0)
	focusOn(f, lOnCommit)
	f.Update(k("space"))
	got, err := f.Dot()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Research.Enabled || got.Research.IdleMinutes != 20 || !got.Research.Web || got.Research.MaxRunsPerDay != 6 ||
		!reflect.DeepEqual(got.Research.Topics, []string{"advisories for package.json", "auth flows"}) {
		t.Fatalf("research %+v", got.Research)
	}
	if got.Schedule.Cron != "0 9 * * 1-5" || got.Schedule.Task != "Review commits since the last run." ||
		!reflect.DeepEqual(got.Schedule.Projects, []string{"attached"}) || got.Schedule.CatchUpHours != 12 || !got.Trigger.OnCommit {
		t.Fatalf("schedule %+v on_commit %v", got.Schedule, got.Trigger.OnCommit)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCronPreviewAndInvalidCronBlocksSave(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	f.now = func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local) } // a Sunday
	if v := f.View(80); !strings.Contains(v, "off (5 fields") {
		t.Fatalf("empty cron: %s", v)
	}
	typeInto(f, lCron, "0 9 * * 1-5", 0)
	if v := f.View(80); !strings.Contains(v, "next: Mon 19 Oct 09:00") {
		t.Fatalf("preview missing:\n%s", v)
	}
	typeInto(f, lCron, "0 25 * * *", 11)
	if v := f.View(80); !strings.Contains(v, "hour \"25\" must be 0-23") {
		t.Fatalf("invalid cron not shown:\n%s", v)
	}
	typeInto(f, lTask, "x", 0)
	d, err := f.Dot()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "schedule cron") {
		t.Fatalf("an invalid cron must block the save: %v", err)
	}
	typeInto(f, lCatchUp, "z", 2)
	if _, err := f.Dot(); err == nil || !strings.Contains(err.Error(), "catch-up hours must be a whole number") {
		t.Fatalf("number error: %v", err)
	}
}

func TestWebWarningForBackendsWithoutWebTools(t *testing.T) {
	d := dots.New("x")
	d.Research.Web = true
	if v := NewForm(d, true).View(100); !strings.Contains(v, "search queries derived from your code") || strings.Contains(v, "has no web tools") {
		t.Fatalf("claude-cli has web tools; the privacy note shows:\n%s", v)
	}
	d.Backend.Kind = "codex-cli"
	if v := NewForm(d, true).View(100); !strings.Contains(v, "codex-cli has no web tools") {
		t.Fatalf("missing warning:\n%s", v)
	}
}

// The editor scrolls: the focused field, the title and the key help stay on
// screen in a short popup.
func TestEditorScrollsToTheFocusedField(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	f.SetHeight(20)
	focusOn(f, lOnCommit)
	v := f.View(80)
	if n := strings.Count(v, "\n") + 1; n > 20 {
		t.Fatalf("%d lines in a 20-line popup", n)
	}
	for _, want := range []string{"NEW DOT", lOnCommit, "ctrl+s save", "↑"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	focusOn(f, "Name")
	if v := f.View(80); !strings.Contains(v, "Name") || !strings.Contains(v, "↓") || strings.Contains(v, lOnCommit) {
		t.Fatalf("back at the top:\n%s", v)
	}
	f.SetHeight(0)
	if v := f.View(80); !strings.Contains(v, "Name") || !strings.Contains(v, lOnCommit) {
		t.Fatal("unknown height renders everything")
	}
}

// Never act on an unseen selection: every field can be focused at every
// height and its label is on screen, with the key help.
func TestFocusedFieldIsVisibleAtEveryHeight(t *testing.T) {
	for _, h := range []int{6, 7, 8, 10, 14, 20, 30, 60} {
		f := NewForm(dots.New("x"), true)
		f.SetHeight(h)
		for range f.fields {
			f.Update(k("tab"))
			v := f.View(80)
			if n := strings.Count(v, "\n") + 1; n > h {
				t.Fatalf("height %d: %d lines", h, n)
			}
			label := strings.TrimSpace(f.fields[f.focus].label)
			if !strings.Contains(v, label) {
				t.Fatalf("height %d: focused %q not on screen:\n%s", h, label, v)
			}
			if !strings.Contains(v, "ctrl+s save") {
				t.Fatalf("height %d: key help missing", h)
			}
		}
	}
}

func TestProactiveToggleLabelsSayReadOnly(t *testing.T) {
	v := NewForm(dots.New("x"), true).View(100)
	if n := strings.Count(v, "read-only"); n < 3 { // repo toggle, research, schedule
		t.Fatalf("read-only labels: %d\n%s", n, v)
	}
	// both proactive features are off by default
	d, err := NewForm(dots.New("x"), true).Dot()
	if err != nil || d.Research.Enabled || d.Schedule.Cron != "" || d.Trigger.OnCommit {
		t.Fatalf("defaults %+v %v", d, err)
	}
}

func TestValidationErrorsShowNextToTheirField(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	typeInto(f, lCron, "*/5 * * * *", 0)
	typeInto(f, lTask, "x", 0)
	typeInto(f, lProjects, "attached, attached", 0)
	typeInto(f, lTopics, "ok", 0)
	v := f.View(120)
	lines := strings.Split(v, "\n")
	find := func(sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}
		return -1
	}
	c, e := find(lCron), find("more often than every 15 minutes")
	if c < 0 || e != c+1 {
		t.Fatalf("cron error not under the cron field (%d, %d):\n%s", c, e, v)
	}
	if strings.Contains(v, "next:") {
		t.Fatalf("a too-frequent cron must not preview:\n%s", v)
	}
	p, e := find(lProjects), find("is a duplicate")
	if p < 0 || e != p+1 {
		t.Fatalf("project error not under its field (%d, %d):\n%s", p, e, v)
	}
	d, _ := f.Dot()
	if d.Validate() == nil {
		t.Fatal("must not validate")
	}
}

func TestNotesAreSanitized(t *testing.T) {
	if got := cleanLine("a\x1b[31mb\nc\td"); strings.ContainsAny(got, "\x1b\n\t") {
		t.Fatalf("%q", got)
	}
}
