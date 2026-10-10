//go:build windows

package backend

import "testing"

// Claude Code matches a Windows path in its POSIX form ("C:\Users\alice
// becomes /c/Users/alice, so use //c/**/.env", permissions docs), so the
// config-dir deny rule must be written that way, never as /C:/....
func TestAbsRuleWritesAWindowsDrivePathInClaudeCodesPosixForm(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\alice\AppData\Roaming\herdr-dots`: "//c/Users/alice/AppData/Roaming/herdr-dots/**",
		`D:\a\_temp\`:               "//d/a/_temp/**",
		`C:\Users\RUNNER~1\AppData`: "//c/Users/RUNNER~1/AppData/**",
	} {
		if got := absRule(in); got != want {
			t.Errorf("absRule(%q) = %q, want %q", in, got, want)
		}
	}
}
