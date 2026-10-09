package main

import "testing"

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
