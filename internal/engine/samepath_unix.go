//go:build unix

package engine

// pathsEqual compares cleaned paths exactly (Unix file systems are case
// sensitive, or sameRepo's file identity check already matched).
func pathsEqual(a, b string) bool { return a == b }
