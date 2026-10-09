package dots

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Security":           "security",
		"Front End  Dot!":    "front-end-dot",
		"  --Spec Critic-- ": "spec-critic",
		"ñandú ✨":            "and",
		"!!!":                "dot",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewIsValidOnceNamedAndPurposed(t *testing.T) {
	d := New("Security")
	if d.Slug != "security" || d.Delivery.Mode != ModeQueue || d.Context.Turns != 3 {
		t.Fatalf("defaults: %+v", d)
	}
	d.Purpose = "find bugs"
	if err := d.Validate(); err != nil {
		t.Fatalf("valid dot rejected: %v", err)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	d := New("")
	d.Purpose = " "
	d.Backend.Kind = "gpt-web"
	d.Delivery.Mode = "shout"
	d.Context.Turns = 0
	d.Context.Verbosity = "loud"
	d.Trigger.Cooldown = -1
	d.Trigger.FileGlobs = []string{"src/[unclosed"}
	d.Budget.MaxCallsPerSession = 0
	err := d.Validate()
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"name", "purpose", "backend", "mode", "turns", "verbosity", "cooldown", "glob", "budget"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestModeNextCycles(t *testing.T) {
	if ModeObserve.Next() != ModeSuggest || ModeInject.Next() != ModeObserve || Mode("junk").Next() != ModeQueue {
		t.Fatal("mode cycle")
	}
}

func TestCouncilNameIsReserved(t *testing.T) {
	for _, name := range []string{"Council", " council ", "COUNCIL!"} {
		d := New(name)
		d.Purpose = "x"
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("%q: want a reserved-name error, got %v", name, err)
		}
	}
	d := New("Security")
	d.Purpose = "x"
	d.Slug = "council" // a hand-made council.toml
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("the council slug is reserved too: %v", err)
	}
	ok := New("Council of Elders")
	ok.Purpose = "x"
	if err := ok.Validate(); err != nil {
		t.Fatalf("only the exact slug is reserved: %v", err)
	}
}

// P9-R5: no display string may carry a control or bidi character (terminal
// escapes, OSC 52 clipboard writes, text that reads backwards in a preview).
func TestValidateRefusesControlAndBidiCharacters(t *testing.T) {
	for field, set := range map[string]func(*Dot){
		"glyph":              func(d *Dot) { d.Glyph = "\u001bc" },
		"name":               func(d *Dot) { d.Name = "Nice\u001b]52;c;ZXZpbA==\u0007" },
		"name (bidi)":        func(d *Dot) { d.Name = "Safe\u202Eexe.txt" },
		"name (isolate)":     func(d *Dot) { d.Name = "a\u2066b" },
		"name (mark)":        func(d *Dot) { d.Name = "a\u200Fb\u061C" },
		"name (C1)":          func(d *Dot) { d.Name = "a\u009bb" },
		"purpose":            func(d *Dot) { d.Purpose = "Review.\n\u001b[2J" },
		"purpose (bidi)":     func(d *Dot) { d.Purpose = "Review \u202Dthis" },
		"backend.model":      func(d *Dot) { d.Backend.Model = "sonnet\u001b[31m" },
		"backend.effort":     func(d *Dot) { d.Backend.Effort = "high\u202E" },
		"backend.args":       func(d *Dot) { d.Backend.Args = []string{"--model", "x\u0007"} },
		"trigger.file_globs": func(d *Dot) { d.Trigger.FileGlobs = []string{"**/*.go\u202E"} },
		"schedule.task":      func(d *Dot) { d.Schedule.Task = "t\u2067" },
		"research.topics":    func(d *Dot) { d.Research.Topics = []string{"go\u200E"} },
	} {
		d := New("Ok")
		d.Purpose = "p"
		set(&d)
		err := d.Validate()
		key, _, _ := strings.Cut(field, " ")
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s: Validate = %v, want an error naming %s", field, err, key)
		}
	}
	d := New("Ünïcode ✨")
	d.Purpose = "Line one.\n\tIndented line two: naïve café, 日本語, emoji 🐛."
	d.Glyph = "✨"
	if err := d.Validate(); err != nil {
		t.Fatalf("plain unicode, newlines and tabs in purpose are fine: %v", err)
	}
	for _, tpl := range Templates() {
		if err := tpl.Validate(); err != nil {
			t.Errorf("template %s: %v", tpl.Slug, err)
		}
	}
}
