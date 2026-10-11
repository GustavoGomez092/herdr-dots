package platform

import "testing"

// Herdr can start plugin commands with a \\?\ path on Windows; a path the
// plugin records (the MCP server command) must not keep that prefix, or the
// same binary reads as two different commands.
func TestStripLongPathPrefix(t *testing.T) {
	for in, want := range map[string]string{
		`\\?\C:\Users\me\herdr-dots\bin\herdr-dots.exe`: `C:\Users\me\herdr-dots\bin\herdr-dots.exe`,
		`\\?\UNC\server\share\bin\herdr-dots.exe`:       `\\server\share\bin\herdr-dots.exe`,
		`C:\Users\me\bin\herdr-dots.exe`:                `C:\Users\me\bin\herdr-dots.exe`,
		`/home/me/bin/herdr-dots`:                       `/home/me/bin/herdr-dots`,
		`\\server\share\x`:                              `\\server\share\x`,
		``:                                              ``,
	} {
		if got := StripLongPathPrefix(in); got != want {
			t.Errorf("StripLongPathPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
