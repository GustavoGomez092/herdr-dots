// Package dots defines a dot (a specialist agent) and stores dot definitions as
// one TOML file per dot in CONFIG_DIR/dots/<slug>.toml.
package dots

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

type Mode string

const (
	ModeObserve Mode = "observe"
	ModeSuggest Mode = "suggest"
	ModeQueue   Mode = "queue"
	ModeInject  Mode = "inject"
)

var Modes = []Mode{ModeObserve, ModeSuggest, ModeQueue, ModeInject}

func (m Mode) Valid() bool { return slices.Contains(Modes, m) }

// Next cycles observe → suggest → queue → inject → observe. Unknown → queue.
func (m Mode) Next() Mode {
	for i, x := range Modes {
		if x == m {
			return Modes[(i+1)%len(Modes)]
		}
	}
	return ModeQueue
}

var (
	BackendKinds = []string{"claude-cli", "codex-cli", KindAnthropicAPI, KindOpenAIAPI, KindOpenRouter, KindOllama}
	Verbosities  = []string{"terse", "normal", "detailed"}
	Colors       = []string{"red", "green", "yellow", "blue", "magenta", "cyan", "white"}
)

type Dot struct {
	Slug       string       `toml:"-"`
	Name       string       `toml:"name"`
	Glyph      string       `toml:"glyph"`
	Color      string       `toml:"color"`
	Origin     string       `toml:"origin,omitempty"` // template or import in a user file; "" = user; project is set at load
	SourcePath string       `toml:"-"`                // project dots: the file the dot was loaded from
	Purpose    string       `toml:"purpose,omitempty"`
	Backend    BackendSpec  `toml:"backend"`
	Delivery   DeliverySpec `toml:"delivery"`
	Trigger    TriggerSpec  `toml:"trigger"`
	Context    ContextSpec  `toml:"context"`
	Budget     BudgetSpec   `toml:"budget"`
	Memory     MemorySpec   `toml:"memory"`
	Research   ResearchSpec `toml:"research"` // Phase 8: idle research
	Schedule   ScheduleSpec `toml:"schedule"` // Phase 8: standing task on a cron schedule

	Capabilities CapabilitySpec `toml:"capabilities"`
}

type BackendSpec struct {
	Kind  string   `toml:"kind"`
	Model string   `toml:"model"`
	Args  []string `toml:"args"`

	// API backends (Phase 4); zero values mean provider defaults and are
	// omitted from the file.
	Effort        string `toml:"effort,omitempty"`        // anthropic-api, openai-api
	MaxTokens     int    `toml:"max_tokens,omitzero"`     // API kinds; 0 = 4096
	BaseURL       string `toml:"base_url,omitempty"`      // ollama, openrouter
	ContextWindow int    `toml:"context_window,omitzero"` // ollama; 0 = 16384
	Fallbacks     string `toml:"fallbacks,omitempty"`     // anthropic-api: default | off
}

type DeliverySpec struct {
	Mode Mode `toml:"mode"`
}

type TriggerSpec struct {
	OnTurn    bool     `toml:"on_turn"`
	FileGlobs []string `toml:"file_globs"`
	Cooldown  int      `toml:"cooldown"`
	OnCommit  bool     `toml:"on_commit"` // Phase 8: review each new commit in the pane's repo
}

type ContextSpec struct {
	Turns     int    `toml:"turns"`
	RepoRead  bool   `toml:"repo_read"`
	Verbosity string `toml:"verbosity"`
}

type BudgetSpec struct {
	MaxCallsPerSession int     `toml:"max_calls_per_session"`
	MaxUSDPerSession   float64 `toml:"max_usd_per_session,omitzero"` // 0 = off
}

// ReservedName and ReservedSlug belong to council mode: council cards use
// them, so no dot may be called "Council" (in any spelling that slugifies to
// "council").
const (
	ReservedName = "Council"
	ReservedSlug = "council"
)

// DefaultMaxCallsPerSession is a new dot's [budget] max_calls_per_session.
const DefaultMaxCallsPerSession = 40

// New returns a dot with every default from the spec filled in.
func New(name string) Dot {
	return Dot{
		Slug:     Slugify(name),
		Name:     name,
		Glyph:    "●",
		Color:    "blue",
		Backend:  BackendSpec{Kind: "claude-cli", Model: "sonnet", Args: []string{}},
		Delivery: DeliverySpec{Mode: ModeQueue},
		Trigger:  TriggerSpec{OnTurn: true, FileGlobs: []string{}},
		Context:  ContextSpec{Turns: 3, RepoRead: true, Verbosity: "normal"},
		Budget:   BudgetSpec{MaxCallsPerSession: DefaultMaxCallsPerSession},
		Memory:   DefaultMemory(),
		Research: DefaultResearch(),
		Schedule: DefaultSchedule(),

		Capabilities: DefaultCapabilities(),
	}
}

// Slugify keeps ASCII letters and digits, turns everything else into single
// dashes, and falls back to "dot" when nothing is left.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "dot"
	}
	return s
}

// Validate returns every problem at once, joined, so the editor can show them all.
func (d Dot) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if strings.TrimSpace(d.Name) == "" {
		add("name is required")
	}
	if Slugify(d.Name) == ReservedSlug || d.Slug == ReservedSlug {
		add("name %q is reserved for council mode; pick another name", ReservedName)
	}
	if strings.TrimSpace(d.Purpose) == "" {
		add("purpose is required")
	}
	switch d.Origin {
	case "", OriginUser, OriginTemplate, OriginImport:
		if IsProjectSlug(d.Slug) {
			add("names starting with %q are reserved for project dots", ProjectPrefix)
		}
	case OriginProject:
		if _, _, ok := SplitProjectSlug(d.Slug); !ok {
			add("project dot slug %q must look like %s<name>--<project>", d.Slug, ProjectPrefix)
		}
	default:
		add("origin %q must be %s, %s or %s", d.Origin, OriginUser, OriginTemplate, OriginImport)
	}
	// 1-2 visible characters; an emoji's joiners and variation selectors
	// don't count, but the whole glyph stays short.
	if n := glyphWidth(d.Glyph); n < 1 || n > 2 || utf8.RuneCountInString(d.Glyph) > 8 {
		add("glyph must be 1-2 characters")
	}
	if !slices.Contains(BackendKinds, d.Backend.Kind) {
		add("backend kind %q must be one of %v", d.Backend.Kind, BackendKinds)
	}
	if bad := DeniedArgs(d.Backend.Kind, d.Backend.Args); len(bad) > 0 {
		add("backend args %v are not allowed: they would override the read-only safety flags", bad)
	}
	if !d.Delivery.Mode.Valid() {
		add("delivery mode %q must be one of %v", d.Delivery.Mode, Modes)
	}
	if d.Context.Turns < 1 || d.Context.Turns > 20 {
		add("context turns must be 1-20, got %d", d.Context.Turns)
	}
	if !slices.Contains(Verbosities, d.Context.Verbosity) {
		add("verbosity %q must be one of %v", d.Context.Verbosity, Verbosities)
	}
	if d.Trigger.Cooldown < 0 {
		add("cooldown must be >= 0")
	}
	for _, g := range d.Trigger.FileGlobs {
		if !doublestar.ValidatePattern(g) {
			add("file glob %q is invalid", g)
		}
	}
	if d.Budget.MaxCallsPerSession < 1 {
		add("budget max_calls_per_session must be >= 1")
	}
	errs = append(errs, textErrors(d)...)
	errs = append(errs, d.Memory.validate()...)
	errs = append(errs, d.Research.validate()...)
	errs = append(errs, d.Schedule.validate()...)
	errs = append(errs, d.Capabilities.validate(d.Backend.Kind)...)
	errs = append(errs, validateAPI(d)...)
	return errors.Join(errs...)
}

// multilineKeys may hold newlines and tabs (CRLF line ends too).
var multilineKeys = []string{"purpose", "schedule.task"}

// textErrors refuses control, bidi and invisible characters in every string
// of the dot (P9-R5, fix round 2): names, glyphs, models and the rest reach
// the terminal, where an escape can rewrite the screen or the clipboard (OSC
// 52); bidi controls make a preview read differently from what runs; and
// invisible format characters (zero-width spaces, tag characters, ...) can
// hide instructions in a purpose that reaches the system prompt. Research
// topics and the schedule task keep their own control-character messages
// (Phase 8).
func textErrors(d Dot) []error {
	var errs []error
	if unsafeText(d.Slug, false) || hiddenText(d.Slug, false) {
		errs = append(errs, fmt.Errorf("slug %q must not contain control, bidi or invisible characters", d.Slug))
	}
	seen := map[string]bool{}
	eachString(reflect.ValueOf(d), "", func(key, s string) {
		if seen[key] {
			return
		}
		multiline := slices.Contains(multilineKeys, key)
		if multiline {
			s = strings.ReplaceAll(s, "\r\n", "\n")
		}
		phase8 := key == "research.topics" || key == "schedule.task"
		if hiddenText(s, key == "glyph") || (!phase8 && unsafeText(s, multiline)) {
			seen[key] = true
			errs = append(errs, fmt.Errorf("%s must not contain control, bidi or invisible characters", key))
		}
	})
	return errs
}

// emojiJoiner reports the zero-width joiner and the text/emoji variation
// selectors, which build an emoji glyph and are allowed there only.
func emojiJoiner(r rune) bool { return r == 0x200D || r == 0xFE0E || r == 0xFE0F }

// hiddenText reports a character that doesn't show as itself: every format
// character (Unicode Cf: the bidi controls U+202A-202E, U+2066-2069, U+200E,
// U+200F and U+061C, zero-width spaces and joiners, the BOM, tag characters
// U+E0000-E007F, soft hyphens, ...), the line and paragraph separators
// U+2028/2029, and variation selectors. In a glyph, a ZWJ and the emoji
// variation selectors are allowed.
func hiddenText(s string, glyph bool) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		if emojiJoiner(r) {
			return !glyph
		}
		return unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
	})
}

// glyphWidth counts a glyph's visible characters: runes other than a ZWJ
// or an emoji variation selector.
func glyphWidth(g string) int {
	n := 0
	for _, r := range g {
		if !emojiJoiner(r) {
			n++
		}
	}
	return n
}
