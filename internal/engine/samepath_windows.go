package engine

import "strings"

// pathsEqual compares cleaned paths case-insensitively: NTFS names are, and
// sameRepo only gets here when a path can't be stat'ed.
func pathsEqual(a, b string) bool { return strings.EqualFold(a, b) }
