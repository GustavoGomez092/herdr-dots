package platform

import "testing"

// CI (windows-latest): "/etc/passwd" is not absolute on Windows (no drive),
// so the tool jail joined it under the project root and looked for
// <root>\etc\passwd. Rooted and drive-relative Windows paths name places
// outside the project; RootRelative reports them and the jail refuses them.
// The rule is OS-neutral, so it is pinned on every OS (toolloop's
// jail_windows_test.go runs it through ResolveInRoot and the write jail).
func TestWindowsRootRelativePaths(t *testing.T) {
	for _, p := range []string{`/etc/passwd`, `\etc\passwd`, `/`, `\`, `\\server\share\x`, `C:x`, `c:..\x`, `Z:`, `C:\x`, `c:/x`} {
		if !windowsRootRelative(p) {
			t.Errorf("windowsRootRelative(%q) = false, want true", p)
		}
	}
	for _, p := range []string{``, `main.go`, `web/app.ts`, `./x`, `..\x`, `ab:c`, `1:x`, `.env:secret`, `:x`} {
		if windowsRootRelative(p) {
			t.Errorf("windowsRootRelative(%q) = true, want false", p)
		}
	}
}
