package main

import (
	"context"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/paths"
)

// memoryStoreFor opens the dot memory store, or returns nil when
// memory_enabled = false (the engine and the manage popup then behave as v1).
// config.toml reloads at runtime, but only in one direction for memory:
// turning it off applies to the next run (the engine reads memory_enabled per
// run); turning it on, and the memory_* limits, need an engine restart.
func memoryStoreFor(p paths.Paths, cfg config.Config, logf func(string, ...any)) *memory.Store {
	if !cfg.MemoryEnabled {
		return nil
	}
	st := memory.Open(p.MemoryDir(), memory.LimitsFromConfig(cfg.MemoryMaxEntries, cfg.MemoryRetentionDays, cfg.MemoryAutoApproveProject))
	st.Logf = logf
	return st
}

// resolveProject identifies a working directory's memory project (git, 5s cap).
func resolveProject(cwd string) (memory.Project, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return memory.ResolveProject(ctx, cwd, memory.ExecGit)
}

// paneProject resolves the memory project of a Herdr pane (the manage
// popup's target) from the pane's working directory.
func paneProject(target string) (memory.Project, error) {
	hc, err := herdr.NewFromEnv()
	if err != nil {
		return memory.Project{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pi, err := hc.PaneGet(ctx, target)
	if err != nil {
		return memory.Project{}, err
	}
	return resolveProject(pi.WorkDir())
}
