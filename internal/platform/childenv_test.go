package platform

import (
	"slices"
	"testing"
)

// The Windows ChildEnv logic, run on every OS: the variable is added once,
// replaces any spelling of an existing one, and the caller's slice is left
// untouched.
func TestNoCwdExeSearchEnv(t *testing.T) {
	in := []string{"PATH=/bin", "nodefaultcurrentdirectoryinexepath=0", "A=b", "NoDefaultCurrentDirectoryInExePath="}
	orig := slices.Clone(in)
	got := noCwdExeSearchEnv(in)
	want := []string{"PATH=/bin", "A=b", NoCwdExeSearch + "=1"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !slices.Equal(in, orig) {
		t.Fatalf("input changed to %q", in)
	}
	if got := noCwdExeSearchEnv(nil); !slices.Equal(got, []string{NoCwdExeSearch + "=1"}) {
		t.Fatalf("empty env: %q", got)
	}
	// A variable whose name merely starts the same way is kept.
	if got := noCwdExeSearchEnv([]string{"NoDefaultCurrentDirectoryInExePathX=1"}); len(got) != 2 {
		t.Fatalf("prefix-named variable dropped: %q", got)
	}
}

// Post-rc: a plugin process can inherit a Claude Code session's variables
// (the session-leak incident). The Setup children (claude mcp add/remove/get,
// codex mcp list, herdr server reload-config) get none of them: every
// CLAUDE* variable goes, in any case, except CLAUDE_CONFIG_DIR, which picks
// the config file the plan previews (Phase 5 decision 29).
func TestWithoutClaudeEnvDropsEveryClaudeVariableButTheConfigDir(t *testing.T) {
	in := []string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_CODE_SSE_PORT=1",
		"CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_CODE_OAUTH_TOKEN=t", "CLAUDE_CODE_USE_BEDROCK=1", "claude_x=1",
		"Claude_Session=2", "CLAUDE_CONFIG_DIR=/c", "HOME=/h", "MY_CLAUDE=keep", "NOEQUALS"}
	orig := slices.Clone(in)
	got := WithoutClaudeEnv(in)
	want := []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/c", "HOME=/h", "MY_CLAUDE=keep", "NOEQUALS"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !slices.Equal(in, orig) {
		t.Fatalf("input changed to %q", in)
	}
}
