package main

import (
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/paths"
)

// Phase 5 final review M9: the detached engine serves every pane, so it
// keeps no pane id and none of the Claude Code session markers of the agent
// whose MCP server happened to start it.
func TestEngineEnvDropsPaneAndClaudeSessionVariables(t *testing.T) {
	p := paths.New("/cfg", "/state", "/run/herdr.sock")
	// The provider and auth settings a claude-cli dot needs stay (backend.CleanEnv
	// keeps them for the same reason).
	keep := []string{"PATH=/bin", "ANTHROPIC_API_KEY=k", "CLAUDE_CONFIG_DIR=/c", "CLAUDE_CODE_USE_BEDROCK=1",
		"CLAUDE_CODE_USE_VERTEX=1", "CLAUDE_CODE_SKIP_BEDROCK_AUTH=1", "CLAUDE_CODE_OAUTH_TOKEN=t", "CLAUDE_CODE_CLIENT_CERT=/cert", "CLAUDE_CODE_GIT_BASH_PATH=/g"}
	env := engineEnv(append([]string{"HERDR_PANE_ID=w1:p1", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDE_CODE_SSE_PORT=1234", "CLAUDE_CODE_CHILD_SESSION=1"}, keep...), p)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if (k == "HERDR_PANE_ID" || k == "CLAUDECODE" || strings.HasPrefix(k, "CLAUDE_CODE_")) && !slices.Contains(keep, kv) {
			t.Errorf("kept %s", kv)
		}
	}
	for _, want := range keep {
		if !slices.Contains(env, want) {
			t.Errorf("dropped %s: %v", want, env)
		}
	}
}
