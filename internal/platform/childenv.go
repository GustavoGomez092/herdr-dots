package platform

import "strings"

// NoCwdExeSearch is the Windows variable that stops cmd.exe (and
// CreateProcess's search for a bare program name) from looking in the current
// directory before PATH. npm's claude.cmd and codex.cmd run "node" when no
// node.exe sits beside them, and a child starts in the pane's project
// directory, so without it a repo-local node.exe/.cmd/.bat would run.
const NoCwdExeSearch = "NoDefaultCurrentDirectoryInExePath"

// WithoutClaudeEnv returns a copy of env without any CLAUDE* variable (the
// name compared in any case), except CLAUDE_CONFIG_DIR. A plugin process can
// inherit a Claude Code session's variables (session markers, a messaging
// token), and the children the Setup screen and the setup commands start
// (claude mcp add/remove/get, codex mcp list, herdr server reload-config)
// must not join that session. CLAUDE_CONFIG_DIR stays: it picks the config
// file `claude mcp add` edits, which is the file the plan previews and backs
// up (mcpinstall.Env.ClaudeConfigPath).
func WithoutClaudeEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if len(name) >= 6 && strings.EqualFold(name[:6], "CLAUDE") && !strings.EqualFold(name, "CLAUDE_CONFIG_DIR") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// noCwdExeSearchEnv returns a copy of env without any spelling of
// NoCwdExeSearch, with NoCwdExeSearch=1 appended. It is ChildEnv's Windows
// logic, kept OS-neutral so every OS can test it.
func noCwdExeSearchEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); strings.EqualFold(name, NoCwdExeSearch) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, NoCwdExeSearch+"=1")
}
