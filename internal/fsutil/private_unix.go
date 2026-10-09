//go:build unix

package fsutil

import "os"

// restrict: Chmod already applied the Unix permission bits.
func restrict(string, os.FileMode) error { return nil }

func rename(from, to string) error { return os.Rename(from, to) }
