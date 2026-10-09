package main

import (
	"testing"

	"herdr-dots/internal/config"
)

func TestResolveTargetPrefersEnvThenContext(t *testing.T) {
	t.Setenv(envTarget, "w1:p9")
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "")
	if got := resolveTarget(); got != "w1:p9" {
		t.Fatalf("env target: %q", got)
	}
	t.Setenv(envTarget, "")
	if got := resolveTarget(); got != "" {
		t.Fatalf("no context should yield empty, got %q", got)
	}
}

func TestNewDotFromConfigUsesDefaults(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultBackend, cfg.DefaultModel = "codex-cli", "gpt-x"
	d := newDotFromConfig(cfg)
	if d.Backend.Kind != "codex-cli" || d.Backend.Model != "gpt-x" {
		t.Fatalf("backend %+v", d.Backend)
	}
}
