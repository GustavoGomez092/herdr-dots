// Package version holds the herdr-dots version and where this binary came
// from. The version lives in the VERSION file next to this one, so the
// install scripts (scripts/build.sh, build.ps1) read the same value.
package version

import (
	_ "embed"
	"fmt"
	"runtime/debug"
	"strings"
)

//go:embed VERSION
var raw string

// Version is the plugin version, for example "1.0.0-rc.1".
var Version = strings.TrimSpace(raw)

// Source and Commit are set with -ldflags -X by the release build
// (Source "release") and by scripts/build.sh (Source "source").
var (
	Source = "source"
	Commit = ""
)

// CommitID is Commit, or the VCS revision Go recorded in the binary, or
// "unknown".
func CommitID() string {
	if Commit != "" {
		return Commit
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			if dirty {
				rev += "+dirty"
			}
			return rev
		}
	}
	return "unknown"
}

// String is the --version line: "herdr-dots 1.0.0-rc.1 (release, commit 1a2b3c4d5e6f)".
func String() string {
	return fmt.Sprintf("herdr-dots %s (%s, commit %s)", Version, Source, CommitID())
}
