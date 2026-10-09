// Package config loads the plugin-wide settings from CONFIG_DIR/config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
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
}

func Default() Config {
	return Config{
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
	}
}

// Load returns Default() when the file is missing. Keys present in the file
// override defaults; absent keys keep them. An unreadable or malformed file
// yields Default() with InjectEnabled=false (the kill switch fails closed)
// plus an error so the caller can log it and keep running.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		c.InjectEnabled = false
		return c, err
	}
	if _, err := toml.Decode(string(data), &c); err != nil {
		c = Default()
		c.InjectEnabled = false
		return c, fmt.Errorf("config %s: %w", path, err)
	}
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
	return c, nil
}

func (c Config) RunTimeout() time.Duration { return time.Duration(c.RunTimeoutSeconds) * time.Second }
func (c Config) Debounce() time.Duration   { return time.Duration(c.TurnDebounceMs) * time.Millisecond }
