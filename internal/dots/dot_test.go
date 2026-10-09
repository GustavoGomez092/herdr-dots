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
