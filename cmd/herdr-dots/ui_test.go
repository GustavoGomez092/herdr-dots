package main

import (
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/config"
)

func TestResolveTargetPrefersEnvThenContext(t *testing.T) {
	dotsPanes := filepath.Join(t.TempDir(), "dotspanes.json")
	t.Setenv(envTarget, "w1:p9")
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "")
	t.Setenv("HERDR_PANE_ID", "")
	if got := resolveTarget(dotsPanes); got != "w1:p9" {
		t.Fatalf("env target: %q", got)
	}
	t.Setenv(envTarget, "")
	if got := resolveTarget(dotsPanes); got != "" {
		t.Fatalf("no context should yield empty, got %q", got)
	}
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"focused_pane_id":"w1:p1","focused_pane_agent":"claude"}`)
	if got := resolveTarget(dotsPanes); got != "w1:p1" {
		t.Fatalf("focused agent pane: %q", got)
	}
	// prefix+d pressed inside the Dots pane targets its main pane.
	os.WriteFile(dotsPanes, []byte(`{"w1:p1":"w1:p7"}`), 0o600)
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"focused_pane_id":"w1:p7","focused_pane_agent":""}`)
	if got := resolveTarget(dotsPanes); got != "w1:p1" {
		t.Fatalf("Dots pane must map to its main pane, got %q", got)
	}
}

func TestTargetFor(t *testing.T) {
	m := map[string]string{"w1:p1": "w1:p7"}
	cases := []struct{ focused, agent, want string }{
		{"w1:p1", "claude", "w1:p1"}, // agent pane
		{"w1:p7", "", "w1:p1"},       // its Dots pane maps back
		{"w1:p3", "", ""},            // plain shell: library only
		{"", "", ""},
		{"w1:p4", "codex", "w1:p4"},
	}
	for _, c := range cases {
		if got := targetFor(c.focused, c.agent, m); got != c.want {
			t.Errorf("targetFor(%q, %q) = %q, want %q", c.focused, c.agent, got, c.want)
		}
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
