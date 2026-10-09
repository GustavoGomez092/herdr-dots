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
	if len(got.Backend.Args) != 0 || got.Backend.Model != "x" { // the kept --model x is folded into backend.model
		t.Fatalf("args %v", got.Backend.Args)
	}
	var caps []string
	for _, c := range changes(cs) {
		caps = append(caps, c.Capability)
	}
	if want := append(slices.Clone(Capabilities), CapArgs, CapBudget); !slices.Equal(caps, want) {
		t.Fatalf("clamps %v, want %v", caps, want)
	}
	ch := changes(cs)
	if ch[len(ch)-2].Keepable || ch[len(ch)-1].Keepable || !ch[0].Keepable {
		t.Fatal("args and budget can't be kept; inject can")
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
	if cs := changes(cs); len(cs) != 3 || cs[0].Capability != CapArgs || cs[1].Capability != CapProjects || cs[1].Keepable || cs[2].Capability != CapBudget {
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
	if len(all.Backend.Args) != 0 || all.Backend.Model != "x" {
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
	if CheckCapabilities([]string{CapArgs}) == nil || CheckCapabilities([]string{CapBudget}) == nil {
		t.Fatal("args and budget can't be kept")
	}
}

// changes drops the preview facts (backend kind and model) from cs.
func changes(cs []Clamp) []Clamp {
	return slices.DeleteFunc(slices.Clone(cs), func(c Clamp) bool { return c.Fact })
}

// keepable is the clamps a developer could keep.
func keepable(cs []Clamp) []Clamp {
	return slices.DeleteFunc(slices.Clone(cs), func(c Clamp) bool { return !c.Keepable })
}

// Someone else's dot can't spend more than a default dot: calls are capped
// at the default and the dollar budget at SharedMaxUSDPerSession (also when
// it was off). Kind and model are reported so a preview can show them.
func TestClampCapsTheBudgetAndReportsBackendFacts(t *testing.T) {
	d := riskyDot()
	d.Budget.MaxCallsPerSession, d.Budget.MaxUSDPerSession = 500, 50
	got, cs := ClampDot(d, Capabilities)
	if got.Budget.MaxCallsPerSession != DefaultMaxCallsPerSession || got.Budget.MaxUSDPerSession != SharedMaxUSDPerSession {
		t.Fatalf("budget %+v", got.Budget)
	}
	var budget, facts []string
	for _, c := range cs {
		switch {
		case c.Fact:
			if c.Keepable || c.Capability != CapBackend {
				t.Errorf("fact %+v", c)
			}
			facts = append(facts, c.String())
		case c.Capability == CapBudget:
			if c.Keepable {
				t.Errorf("budget is keepable: %+v", c)
			}
			budget = append(budget, c.String())
		}
	}
	if want := []string{"budget: budget.max_calls_per_session 500 → 40", "budget: budget.max_usd_per_session 50 → 2"}; !slices.Equal(budget, want) {
		t.Errorf("budget clamps %q, want %q", budget, want)
	}
	if want := []string{"backend: backend.kind = claude-cli", "backend: backend.model = x"}; !slices.Equal(facts, want) {
		t.Errorf("facts %q, want %q", facts, want)
	}
	if d.Budget.MaxCallsPerSession != 500 {
		t.Fatal("ClampDot changed its input")
	}

	off := New("Off")
	off.Purpose = "p"
	got, cs = ClampDot(off, nil)
	if got.Budget.MaxUSDPerSession != SharedMaxUSDPerSession || !slices.ContainsFunc(cs, func(c Clamp) bool {
		return c.Capability == CapBudget && c.Field == "budget.max_usd_per_session" && c.From == "0 (off)"
	}) {
		t.Fatalf("an off dollar budget gets the shared default: %+v %+v", got.Budget, cs)
	}

	low := New("Low")
	low.Purpose = "p"
	low.Budget = BudgetSpec{MaxCallsPerSession: 5, MaxUSDPerSession: 0.5}
	low.Backend.Kind, low.Backend.Model = KindAnthropicAPI, ""
	got, cs = ClampDot(low, nil)
	if got.Budget != low.Budget || slices.ContainsFunc(cs, func(c Clamp) bool { return c.Capability == CapBudget }) {
		t.Fatalf("a budget under the caps stays: %+v %+v", got.Budget, cs)
	}
	if !slices.ContainsFunc(cs, func(c Clamp) bool { return c.Fact && c.String() == "backend: backend.model = (default)" }) {
		t.Fatalf("an empty model is shown as the default: %+v", cs)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
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

	"Budget.MaxCallsPerSession": CapBudget,
	"Budget.MaxUSDPerSession":   CapBudget,
}

// reviewedSafe are the Dot fields a shared dot may set as it likes, and why.
// A field in neither map fails TestClampDotHandlesEveryDotField: whoever adds
// it decides whether ClampDot must turn it off.
var reviewedSafe = map[string]string{
	"Slug":                             "set by the loader (project dots: proj--<file>--<project>)",
	"Name":                             "display text: Validate refuses control and bidi characters (P9-R5)",
	"Glyph":                            "display text, 1-2 runes: Validate refuses control and bidi characters (P9-R5)",
	"Color":                            "allow-listed by Validate",
	"Origin":                           "set by the loader; DecodeStrict clears it (P9-R7)",
	"SourcePath":                       "set by the loader, never serialized",
	"Purpose":                          "the prompt, shown in full before trust or import; no control or bidi characters but newline and tab (P9-R5)",
	"Backend.Kind":                     "allow-listed; runs on the developer's own providers and keys; reported as a fact in the clamp list",
	"Backend.Model":                    "no control or bidi characters (P9-R5); reported as a fact in the clamp list",
	"Backend.Effort":                   "allow-listed by Validate",
	"Backend.MaxTokens":                "bounded by Validate",
	"Backend.ContextWindow":            "bounded by Validate",
	"Backend.Fallbacks":                "allow-listed by Validate",
	"Trigger.OnTurn":                   "a read-only review per turn (the default)",
	"Trigger.FileGlobs":                "narrows when it runs; valid globs, no control or bidi characters",
	"Trigger.Cooldown":                 "narrows when it runs",
	"Context.Turns":                    "bounded by Validate",
	"Context.RepoRead":                 "read-only repo access (the default)",
	"Context.Verbosity":                "allow-listed",
	"Memory.Enabled":                   "reads approved memory into the user prompt",
	"Memory.Learn":                     "model writes land proposed (Phase 2 op rules)",
	"Memory.ReadProject":               "reads approved project memory",
	"Memory.EveryTurns":                "bounded by Validate",
	"Memory.MaxPassesPerSession":       "bounded by Validate",
	"Memory.PromptChars":               "bounded by Validate",
	"Research.IdleMinutes":             "inert while research is off",
	"Research.MaxRunsPerDay":           "inert while research is off",
	"Research.Topics":                  "inert while research is off; one line, no control or bidi characters; sanitized for prompts",
	"Schedule.Task":                    "inert while cron is off; no control or bidi characters but newline and tab; sanitized for prompts",
	"Schedule.CatchUpHours":            "inert while cron is off",
	"Capabilities.WriteTimeoutSeconds": "inert while write is off",
	"Capabilities.MaxPatchBytes":       "inert while write is off",
	"Capabilities.MaxFiles":            "inert while write is off",
}

// dotFields lists the leaf fields of struct type t by path. It recurses into
// structs, and into the struct elements of pointers, slices, arrays and maps
// ("List[].Field"), so a risky flag can't hide one level down.
func dotFields(t reflect.Type, prefix string) []reflect.StructField {
	var out []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		elem, path := f.Type, prefix+f.Name
	unwrap:
		for {
			switch elem.Kind() {
			case reflect.Pointer:
				elem = elem.Elem()
			case reflect.Slice, reflect.Array, reflect.Map:
				inner := elem.Elem()
				for inner.Kind() == reflect.Pointer {
					inner = inner.Elem()
				}
				if inner.Kind() != reflect.Struct {
					break unwrap
				}
				elem, path = inner, path+"[]"
			default:
				break unwrap
			}
		}
		if elem.Kind() == reflect.Struct {
			out = append(out, dotFields(elem, path+".")...)
			continue
		}
		f.Name, f.Type = path, elem
		out = append(out, f)
	}
	return out
}

type probeInner struct{ C bool }

type nestedProbe struct {
	A       bool
	List    []struct{ B bool }
	Arr     [2]probeInner
	Map     map[string]probeInner
	Ptr     *probeInner
	PtrList []*probeInner
	Tags    []string
}

func TestDotFieldsRecursesIntoContainersOfStructs(t *testing.T) {
	var got []string
	for _, f := range dotFields(reflect.TypeOf(nestedProbe{}), "") {
		got = append(got, f.Name+":"+f.Type.Kind().String())
	}
	want := []string{"A:bool", "List[].B:bool", "Arr[].C:bool", "Map[].C:bool", "Ptr.C:bool", "PtrList[].C:bool", "Tags:slice"}
	if !slices.Equal(got, want) {
		t.Fatalf("fields %v, want %v", got, want)
	}
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
	caps = slices.Compact(caps)
	want := append(slices.Clone(Capabilities), CapArgs, CapProjects, CapBudget)
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
		if cs := keepable(cs); len(cs) != 1 || cs[0].Capability != capability {
			t.Errorf("%s: clamps %+v", path, cs)
		}
		kept, cs := ClampDot(d, []string{capability})
		if !fieldByPath(reflect.ValueOf(kept), path).Bool() || len(keepable(cs)) != 0 {
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
