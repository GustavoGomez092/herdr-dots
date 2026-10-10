package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestSetupChildPrintsEnv is not a test: runSetupChild starts this test
// binary as a stand-in for `claude`, and it prints its environment.
func TestSetupChildPrintsEnv(t *testing.T) {
	if os.Getenv("HERDR_DOTS_TEST_PRINT_ENV") != "1" {
		t.Skip("helper process for TestTheClaudeMCPAddChildGetsNoClaudeVariables")
	}
	for _, kv := range os.Environ() {
		fmt.Println(kv)
	}
}

// The session-leak incident: a plugin process can inherit a Claude Code
// session's variables. The children setup-mcp, setup-keys, setup and the
// Setup screen start (claude mcp add first of all) get no CLAUDE* variable,
// except CLAUDE_CONFIG_DIR, which picks the file the plan previews, and
// CLAUDE_CODE_GIT_BASH_PATH (Windows claude needs it).
func TestTheClaudeMCPAddChildGetsNoClaudeVariables(t *testing.T) {
	t.Setenv("HERDR_DOTS_TEST_PRINT_ENV", "1")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("CLAUDE_CODE_SSE_PORT", "4242")
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDE_CODE_MESSAGING_TOKEN", "tok")
	t.Setenv("CLAUDE_AGENT_SDK_VERSION", "9")
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("CLAUDE_CODE_GIT_BASH_PATH", "/git/bash")
	out, err := runSetupChild([]string{os.Args[0], "-test.run=^TestSetupChildPrintsEnv$", "-test.v"})
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(out, "HERDR_DOTS_TEST_PRINT_ENV=1") {
		t.Fatalf("the helper didn't run:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		name, _, ok := strings.Cut(strings.TrimSpace(l), "=")
		if ok && strings.HasPrefix(strings.ToUpper(name), "CLAUDE") && name != "CLAUDE_CONFIG_DIR" && name != "CLAUDE_CODE_GIT_BASH_PATH" {
			t.Errorf("the child got %s", l)
		}
	}
	if !strings.Contains(out, "CLAUDE_CODE_GIT_BASH_PATH=/git/bash") {
		t.Errorf("CLAUDE_CODE_GIT_BASH_PATH must reach the child:\n%s", out)
	}
	if !strings.Contains(out, "CLAUDE_CONFIG_DIR="+dir) {
		t.Errorf("CLAUDE_CONFIG_DIR must reach claude mcp add (the plan previews its file):\n%s", out)
	}
}
