// Package dots defines a dot (a specialist agent) and stores dot definitions as
// one TOML file per dot in CONFIG_DIR/dots/<slug>.toml.
package dots

import (
	"errors"
	"fmt"
	"slices"
	"strings"
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
	Slug     string       `toml:"-"`
	Name     string       `toml:"name"`
	Glyph    string       `toml:"glyph"`
	Color    string       `toml:"color"`
	Purpose  string       `toml:"purpose,omitempty"`
	Backend  BackendSpec  `toml:"backend"`
	Delivery DeliverySpec `toml:"delivery"`
	Trigger  TriggerSpec  `toml:"trigger"`
	Context  ContextSpec  `toml:"context"`
	Budget   BudgetSpec   `toml:"budget"`
	Memory   MemorySpec   `toml:"memory"`
	Research ResearchSpec `toml:"research"` // Phase 8: idle research
	Schedule ScheduleSpec `toml:"schedule"` // Phase 8: standing task on a cron schedule

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
		Budget:   BudgetSpec{MaxCallsPerSession: 40},
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
	if n := utf8.RuneCountInString(d.Glyph); n < 1 || n > 2 {
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
	errs = append(errs, d.Memory.validate()...)
	errs = append(errs, d.Research.validate()...)
	errs = append(errs, d.Schedule.validate()...)
	errs = append(errs, d.Capabilities.validate(d.Backend.Kind)...)
	errs = append(errs, validateAPI(d)...)
	return errors.Join(errs...)
}
