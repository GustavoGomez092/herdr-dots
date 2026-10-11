package platform

import "strings"

// StripLongPathPrefix drops Windows' \\?\ (and \\?\UNC\) long-path prefix,
// keeping the path's case: Herdr can start plugin commands with such a path,
// and a path the plugin records (the MCP server command) must read the same
// however the plugin was started. Other paths are returned unchanged.
func StripLongPathPrefix(p string) string {
	switch {
	case strings.HasPrefix(p, `\\?\UNC\`):
		return `\\` + p[len(`\\?\UNC\`):]
	case strings.HasPrefix(p, `\\?\`):
		return p[len(`\\?\`):]
	}
	return p
}
