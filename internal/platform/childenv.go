package platform

import "strings"

// NoCwdExeSearch is the Windows variable that stops cmd.exe (and
// CreateProcess's search for a bare program name) from looking in the current
// directory before PATH. npm's claude.cmd and codex.cmd run "node" when no
// node.exe sits beside them, and a child starts in the pane's project
// directory, so without it a repo-local node.exe/.cmd/.bat would run.
const NoCwdExeSearch = "NoDefaultCurrentDirectoryInExePath"

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
