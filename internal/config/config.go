// Package config loads the plugin-wide settings from CONFIG_DIR/config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bmatcuk/doublestar/v4"
)

type Config struct {
	MCP MCPConfig `toml:"mcp"` // [mcp]: the Dots MCP server (Phase 5, mcp.go)

	DefaultBackend     string `toml:"default_backend"`
	DefaultModel       string `toml:"default_model"`
	SessionBudgetCalls int    `toml:"session_budget_calls"`
	MaxParallelRuns    int    `toml:"max_parallel_runs"`
	RunTimeoutSeconds  int    `toml:"run_timeout_seconds"`
	TurnDebounceMs     int    `toml:"turn_debounce_ms"`
	InjectEnabled      bool   `toml:"inject_enabled"`

	MemoryEnabled            bool `toml:"memory_enabled"`              // global memory kill switch
	MemoryRetentionDays      int  `toml:"memory_retention_days"`       // unused model/outcome memory is archived after this
	MemoryMaxEntries         int  `toml:"memory_max_entries"`          // per dot scope; project 1.5x, global 0.5x
	MemoryAutoApproveProject bool `toml:"memory_auto_approve_project"` // model-written project memory skips approval

	Council CouncilConfig `toml:"council"` // [council] table (Phase 3)

	ProactiveEnabled  bool    `toml:"proactive_enabled"`   // kill switch: idle research, schedules and commit triggers
	DailyBudgetUSD    float64 `toml:"daily_budget_usd"`    // every background run, every session, per local day
	CommitPollSeconds int     `toml:"commit_poll_seconds"` // how often on_commit dots check HEAD

	WriteEnabled        bool `toml:"write_enabled"`         // global switch for write-capable dots (drafts); off by default
	MaxConcurrentDrafts int  `toml:"max_concurrent_drafts"` // drafts running at once, engine-wide (1-8)

	// Phase 4: API backends and dollar budgets.
	SessionBudgetUSD float64                   `toml:"session_budget_usd"` // per pane; 0 = off
	Providers        map[string]ProviderConfig `toml:"providers"`          // [providers.<name>]
	ToolDenyGlobs    []string                  `toml:"tool_deny_globs"`    // added to the built-in deny-list
	InvalidDenyGlobs []string                  `toml:"-"`                  // tool_deny_globs entries that aren't valid globs (RepoToolsOff)
	PricesFile       string                    `toml:"prices_file"`        // "" = CONFIG_DIR/prices.toml

	ProjectDotsEnabled bool `toml:"project_dots_enabled"` // Phase 9: load trusted .herdr-dots/dots from repositories; false on a malformed file

	// UnknownKeys are the keys config.toml sets that no setting reads (a
	// typo), and "providers.<name>" for a provider that doesn't exist,
	// sorted; nil when there are none. The rest of the file still applies.
	UnknownKeys []string `toml:"-"`
}

// KnownProviders are the [providers.<name>] tables a backend reads
// (backend.ProviderFor).
var KnownProviders = []string{"anthropic", "openai", "openrouter", "ollama"}

// ProviderConfig is one [providers.<name>] table (anthropic, openai,
// openrouter, ollama). Keys never live here: api_key_env names a variable.
type ProviderConfig struct {
	APIKeyEnv    string            `toml:"api_key_env"`
	BaseURL      string            `toml:"base_url"`
	ExtraHeaders map[string]string `toml:"extra_headers"`
}

// Provider returns the [providers.<name>] table (zero value when absent).
func (c Config) Provider(name string) ProviderConfig { return c.Providers[name] }

// Proactive defaults ([research], [schedule] and on_commit work).
const (
	DefaultDailyBudgetUSD    = 1.00
	DefaultCommitPollSeconds = 15
)

// Council merge modes ([council] merge).
const (
	MergeChair = "chair" // a chair run merges the answers; plain merge if it fails
	MergePlain = "plain" // always the deterministic plain merge
)

// CouncilConfig is the [council] table.
type CouncilConfig struct {
	ChairBackend string `toml:"chair_backend"` // "" = default_backend
	ChairModel   string `toml:"chair_model"`   // "" = default_model
	Rounds       int    `toml:"rounds"`        // 1, or 2 for dot-to-dot cross-review
	MaxMembers   int    `toml:"max_members"`   // 2-16
	Merge        string `toml:"merge"`         // chair | plain
}

func DefaultCouncil() CouncilConfig {
	return CouncilConfig{Rounds: 1, MaxMembers: 8, Merge: MergeChair}
}

// Normalized fills unset values with the defaults and clamps the rest:
// rounds 1-2, max_members 2-16, and any merge mode but "plain" means "chair".
func (c CouncilConfig) Normalized() CouncilConfig {
	d := DefaultCouncil()
	if c.Rounds == 0 {
		c.Rounds = d.Rounds
	}
	if c.MaxMembers == 0 {
		c.MaxMembers = d.MaxMembers
	}
	c.Rounds = min(max(c.Rounds, 1), 2)
	c.MaxMembers = min(max(c.MaxMembers, 2), 16)
	if strings.ToLower(strings.TrimSpace(c.Merge)) == MergePlain {
		c.Merge = MergePlain
	} else {
		c.Merge = MergeChair
	}
	c.ChairBackend, c.ChairModel = strings.TrimSpace(c.ChairBackend), strings.TrimSpace(c.ChairModel)
	return c
}

func Default() Config {
	return Config{
		MCP:                DefaultMCP(),
		Council:            DefaultCouncil(),
		DefaultBackend:     "claude-cli",
		DefaultModel:       "sonnet",
		SessionBudgetCalls: 120,
		MaxParallelRuns:    3,
		RunTimeoutSeconds:  120,
		TurnDebounceMs:     1500,
		InjectEnabled:      true,

		MemoryEnabled:       true,
		MemoryRetentionDays: 180,
		MemoryMaxEntries:    200,

		ProactiveEnabled:  true,
		DailyBudgetUSD:    DefaultDailyBudgetUSD,
		CommitPollSeconds: DefaultCommitPollSeconds,

		MaxConcurrentDrafts: 2,

		ProjectDotsEnabled: true,
	}
}

// Load returns Default() when the file is missing. Keys present in the file
// override defaults; absent keys keep them. An unreadable or malformed file
// yields Default() with InjectEnabled, MemoryEnabled, WriteEnabled,
// ProactiveEnabled and ProjectDotsEnabled false (the kill switches fail
// closed) plus an error so the
// caller can log it and keep running.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		c.InjectEnabled, c.MemoryEnabled, c.WriteEnabled, c.ProactiveEnabled = false, false, false, false
		c.ProjectDotsEnabled = false
		return c, err
	}
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		c = Default()
		c.InjectEnabled, c.MemoryEnabled, c.WriteEnabled, c.ProactiveEnabled = false, false, false, false
		c.ProjectDotsEnabled = false
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		if k.String() == "version" {
			continue // the file's format version (migrations) is never a typo
		}
		c.UnknownKeys = append(c.UnknownKeys, k.String())
	}
	for name := range c.Providers {
		if !slices.Contains(KnownProviders, name) {
			c.UnknownKeys = append(c.UnknownKeys, "providers."+name)
		}
	}
	sort.Strings(c.UnknownKeys)
	if c.MaxParallelRuns < 1 {
		c.MaxParallelRuns = 1
	}
	if c.RunTimeoutSeconds < 10 {
		c.RunTimeoutSeconds = 10
	}
	if c.TurnDebounceMs < 0 {
		c.TurnDebounceMs = 0
	}
	if c.SessionBudgetCalls < 1 {
		c.SessionBudgetCalls = 1
	}
	if c.MemoryRetentionDays < 1 {
		c.MemoryRetentionDays = 1
	}
	c.MemoryMaxEntries = min(max(c.MemoryMaxEntries, 10), 10000)
	c.MCP = c.MCP.Normalized()
	c.Council = c.Council.Normalized()
	// The daily budget always caps background work: zero, negative, NaN or
	// infinite values mean the default, never "unlimited".
	if !(c.DailyBudgetUSD > 0) || math.IsInf(c.DailyBudgetUSD, 0) {
		c.DailyBudgetUSD = DefaultDailyBudgetUSD
	}
	c.CommitPollSeconds = min(max(c.CommitPollSeconds, 5), 600)
	c.MaxConcurrentDrafts = min(max(c.MaxConcurrentDrafts, 1), 8)
	if math.IsNaN(c.SessionBudgetUSD) || math.IsInf(c.SessionBudgetUSD, 0) || c.SessionBudgetUSD < 0 {
		c.SessionBudgetUSD = 0
	}
	// A deny glob that doesn't parse can't deny anything, so it fails closed
	// (ruling P10-R7): the valid ones are kept, the invalid ones recorded, and
	// RepoToolsOff turns repository tools, drafts and research off.
	var globs, invalid []string
	for _, g := range c.ToolDenyGlobs {
		if doublestar.ValidatePattern(g) {
			globs = append(globs, g)
		} else {
			invalid = append(invalid, g)
		}
	}
	c.ToolDenyGlobs, c.InvalidDenyGlobs = globs, invalid
	for name, p := range c.Providers {
		c.Providers[name] = p.sanitized()
	}
	return c, nil
}

var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// secretHeaders can never be set from config.toml (keys don't live there).
var secretHeaders = map[string]bool{
	"authorization": true, "x-api-key": true, "api-key": true, "proxy-authorization": true,
}

// sanitized drops what must not live in config.toml: a literal key in
// api_key_env, an unusable base_url, and credential headers.
func (p ProviderConfig) sanitized() ProviderConfig {
	if !envVarName.MatchString(p.APIKeyEnv) {
		p.APIKeyEnv = ""
	}
	if p.BaseURL != "" {
		u, err := url.Parse(p.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" {
			p.BaseURL = ""
		}
	}
	if len(p.ExtraHeaders) > 0 {
		kept := make(map[string]string, len(p.ExtraHeaders))
		for k, v := range p.ExtraHeaders {
			if !secretHeaders[strings.ToLower(strings.TrimSpace(k))] {
				kept[k] = v
			}
		}
		p.ExtraHeaders = kept
	}
	return p
}

// RepoToolsOff reports a tool_deny_globs entry that isn't a valid glob:
// until it is fixed, no run reads the repository, and drafts and research
// are off (the deny list must hold everything the developer listed).
func (c Config) RepoToolsOff() bool { return len(c.InvalidDenyGlobs) > 0 }

func (c Config) RunTimeout() time.Duration { return time.Duration(c.RunTimeoutSeconds) * time.Second }
func (c Config) Debounce() time.Duration   { return time.Duration(c.TurnDebounceMs) * time.Millisecond }
