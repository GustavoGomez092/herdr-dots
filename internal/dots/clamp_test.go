package dots

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/platform/platformtest"
)

// riskyDot asks for every capability a shared dot can ask for.
func riskyDot() Dot {
	d := New("Risky")
	d.Purpose = "p"
	d.Delivery.Mode = ModeInject
	d.Capabilities.Write = true
	d.Capabilities.MCP = true
	d.Research.Enabled, d.Research.Web = true, true
	d.Schedule.Cron = "0 9 * * 1-5"
	d.Trigger.OnCommit = true
	d.Backend.BaseURL = "https://example.invalid/v1"
	d.Backend.Args = []string{"--model", "x", "--dangerously-skip-permissions", "--add-dir", "/etc"}
	return d
}

func TestClampDotTurnsEveryRiskyCapabilityOff(t *testing.T) {
	d := riskyDot()
	got, cs := ClampDot(d, nil)
	if got.Delivery.Mode != ModeQueue || got.Capabilities.Write || got.Capabilities.MCP || got.Research.Enabled ||
		got.Research.Web || got.Schedule.Cron != "" || got.Trigger.OnCommit || got.Backend.BaseURL != "" {
		t.Fatalf("not clamped: %+v", got)
	}
	if !slices.Equal(got.Backend.Args, []string{"--model", "x"}) {
		t.Fatalf("args %v", got.Backend.Args)
	}
	var caps []string
	for _, c := range cs {
		caps = append(caps, c.Capability)
	}
	if want := append(slices.Clone(Capabilities), CapArgs); !slices.Equal(caps, want) {
		t.Fatalf("clamps %v, want %v", caps, want)
	}
	if cs[len(cs)-1].Keepable || !cs[0].Keepable {
		t.Fatal("args can't be kept; inject can")
	}
	if d.Delivery.Mode != ModeInject || len(d.Backend.Args) != 5 {
		t.Fatal("ClampDot changed its input")
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("a clamped dot validates: %v", err)
	}
}

// A shared dot's schedule may only target the projects it is attached to:
// absolute paths name directories on the author's machine (or a guess at
// yours), and keeping "schedule" never keeps them.
func TestClampRemovesAbsoluteScheduleProjects(t *testing.T) {
	d := riskyDot()
	d.Backend.BaseURL = "" // claude-cli has none; keeping it would not validate
	d.Schedule.Task = "t"
	d.Schedule.Projects = []string{ScheduleAttached, platformtest.Abs("home", "me", "secret-repo")}
	got, cs := ClampDot(d, Capabilities)
	if !slices.Equal(got.Schedule.Projects, []string{ScheduleAttached}) || got.Schedule.Cron == "" {
		t.Fatalf("schedule %+v", got.Schedule)
	}
	if len(cs) != 2 || cs[0].Capability != CapArgs || cs[1].Capability != CapProjects || cs[1].Keepable {
		t.Fatalf("clamps %+v", cs)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestKeepRestoresOnlyTheNamedCapability(t *testing.T) {
	got, cs := ClampDot(riskyDot(), []string{CapInject})
	if got.Delivery.Mode != ModeInject || got.Capabilities.Write {
		t.Fatalf("keep inject: %+v", got)
	}
	for _, c := range cs {
		if c.Capability == CapInject {
			t.Fatal("a kept capability is not a clamp")
		}
	}
	all, _ := ClampDot(riskyDot(), Capabilities)
	if len(all.Backend.Args) != 2 {
		t.Fatal("keeping everything still removes denied args")
	}
}

// Clearing write rebuilds CapabilitySpec (the write-tool guard allows no
// assignment to .Write outside drafts); every other field must survive.
func TestClampKeepsEveryOtherCapabilityField(t *testing.T) {
	d := riskyDot()
	d.Capabilities.WriteTimeoutSeconds, d.Capabilities.MaxPatchBytes, d.Capabilities.MaxFiles = 900, 5000, 7
	got, _ := ClampDot(d, []string{CapMCP})
	want := d.Capabilities
	want.Write = false
	if got.Capabilities != want {
		t.Fatalf("capabilities %+v, want %+v", got.Capabilities, want)
	}
}

func TestRequestedAndCheckCapabilities(t *testing.T) {
	if got := Requested(riskyDot()); !slices.Equal(got, Capabilities) {
		t.Fatalf("requested %v", got)
	}
	plain := New("Plain")
	if got := Requested(plain); !slices.Equal(got, []string{CapMCP}) {
		t.Fatalf("a default dot only asks for mcp (on by default): %v", got)
	}
	if CheckCapabilities([]string{"inject", "web"}) != nil || CheckCapabilities([]string{"shell"}) == nil {
		t.Fatal("CheckCapabilities")
	}
}

// clampedFields are the Dot fields ClampDot turns off (or strips), by the
// capability that keeps them.
var clampedFields = map[string]string{
	"Delivery.Mode":      CapInject,
	"Capabilities.Write": CapWrite,
	"Capabilities.MCP":   CapMCP,
	"Research.Enabled":   CapResearch,
	"Research.Web":       CapWeb,
	"Schedule.Cron":      CapSchedule,
	"Trigger.OnCommit":   CapCommit,
	"Backend.BaseURL":    CapBaseURL,
	"Backend.Args":       CapArgs,
	"Schedule.Projects":  CapProjects,
}

// reviewedSafe are the Dot fields a shared dot may set as it likes, and why.
// A field in neither map fails TestClampDotHandlesEveryDotField: whoever adds
// it decides whether ClampDot must turn it off.
var reviewedSafe = map[string]string{
	"Slug":                             "set by the loader",
	"Name":                             "display text",
	"Glyph":                            "display text",
	"Color":                            "display, allow-listed",
	"Origin":                           "set by the loader",
	"SourcePath":                       "set by the loader, never serialized",
	"Purpose":                          "the prompt; shown in full before trust or import",
	"Backend.Kind":                     "the developer's own configured providers and keys",
	"Backend.Model":                    "a model name",
	"Backend.Effort":                   "allow-listed",
	"Backend.MaxTokens":                "bounded by Validate",
	"Backend.ContextWindow":            "bounded by Validate",
	"Backend.Fallbacks":                "allow-listed",
	"Trigger.OnTurn":                   "a read-only review per turn (the default)",
	"Trigger.FileGlobs":                "narrows when it runs",
	"Trigger.Cooldown":                 "narrows when it runs",
	"Context.Turns":                    "bounded by Validate",
	"Context.RepoRead":                 "read-only repo access (the default)",
	"Context.Verbosity":                "allow-listed",
	"Budget.MaxCallsPerSession":        "a cap",
	"Budget.MaxUSDPerSession":          "a cap",
	"Memory.Enabled":                   "reads approved memory into the user prompt",
	"Memory.Learn":                     "model writes land proposed (Phase 2 op rules)",
	"Memory.ReadProject":               "reads approved project memory",
	"Memory.EveryTurns":                "bounded by Validate",
	"Memory.MaxPassesPerSession":       "bounded by Validate",
	"Memory.PromptChars":               "bounded by Validate",
	"Research.IdleMinutes":             "inert while research is off",
	"Research.MaxRunsPerDay":           "inert while research is off",
	"Research.Topics":                  "inert while research is off; sanitized for prompts",
	"Schedule.Task":                    "inert while cron is off; sanitized for prompts",
	"Schedule.CatchUpHours":            "inert while cron is off",
	"Capabilities.WriteTimeoutSeconds": "inert while write is off",
	"Capabilities.MaxPatchBytes":       "inert while write is off",
	"Capabilities.MaxFiles":            "inert while write is off",
}

func dotFields(t reflect.Type, prefix string) []reflect.StructField {
	var out []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		f.Name = prefix + f.Name
		if f.Type.Kind() == reflect.Struct {
			out = append(out, dotFields(f.Type, f.Name+".")...)
			continue
		}
		out = append(out, f)
	}
	return out
}

// Fail closed: every field of Dot is either clamped or reviewed as safe, so a
// risky field added later can't reach a shared dot unclamped.
func TestClampDotHandlesEveryDotField(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range dotFields(reflect.TypeOf(Dot{}), "") {
		seen[f.Name] = true
		_, clamped := clampedFields[f.Name]
		_, safe := reviewedSafe[f.Name]
		switch {
		case clamped && safe:
			t.Errorf("%s is both clamped and reviewed safe", f.Name)
		case !clamped && !safe:
			t.Errorf("new Dot field %s: make ClampDot turn it off (and add it to clampedFields) or add it to reviewedSafe with the reason", f.Name)
		}
		if !clamped && strings.HasPrefix(f.Name, "Capabilities.") && f.Type.Kind() == reflect.Bool {
			t.Errorf("%s: every capability flag must be clamped", f.Name)
		}
	}
	for name := range clampedFields {
		if !seen[name] {
			t.Errorf("clampedFields names %s, which Dot no longer has", name)
		}
	}
	for name := range reviewedSafe {
		if !seen[name] {
			t.Errorf("reviewedSafe names %s, which Dot no longer has", name)
		}
	}
	var caps []string
	for _, c := range clampedFields {
		caps = append(caps, c)
	}
	slices.Sort(caps)
	want := append(slices.Clone(Capabilities), CapArgs, CapProjects)
	slices.Sort(want)
	if !slices.Equal(caps, want) {
		t.Errorf("clampedFields covers %v, want every capability once: %v", caps, want)
	}
}

func fieldByPath(v reflect.Value, path string) reflect.Value {
	for _, name := range strings.Split(path, ".") {
		v = v.FieldByName(name)
	}
	return v
}

// Every clamped flag, set on its own, comes back off with one clamp naming it.
func TestClampTurnsOffEveryClampedFlag(t *testing.T) {
	for path, capability := range clampedFields {
		d := New("Flag")
		d.Purpose = "p"
		f := fieldByPath(reflect.ValueOf(&d).Elem(), path)
		if f.Kind() != reflect.Bool {
			continue // modes, strings and lists: TestClampDotTurnsEveryRiskyCapabilityOff
		}
		if path != "Capabilities.MCP" {
			d.Capabilities.MCP = false // on by default
		}
		f.SetBool(true)
		got, cs := ClampDot(d, nil)
		if fieldByPath(reflect.ValueOf(got), path).Bool() {
			t.Errorf("%s still on", path)
		}
		if len(cs) != 1 || cs[0].Capability != capability || !cs[0].Keepable {
			t.Errorf("%s: clamps %+v", path, cs)
		}
		kept, cs := ClampDot(d, []string{capability})
		if !fieldByPath(reflect.ValueOf(kept), path).Bool() || len(cs) != 0 {
			t.Errorf("%s: keeping %s: %+v", path, capability, cs)
		}
	}
}

func TestClampStringsShowWhatChanged(t *testing.T) {
	_, cs := ClampDot(riskyDot(), nil)
	if s := cs[0].String(); s != "inject: delivery.mode inject → queue" {
		t.Fatalf("inject clamp %q", s)
	}
	for _, c := range cs {
		if c.Capability == CapSchedule && c.String() != `schedule: schedule.cron "0 9 * * 1-5" → (off)` {
			t.Fatalf("schedule clamp %q", c.String())
		}
	}
}
