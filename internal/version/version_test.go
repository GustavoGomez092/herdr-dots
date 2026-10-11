package version

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestVersionIsTheReleaseCandidate(t *testing.T) {
	// Phase 10 ships a release candidate. 1.0.0 is tagged only by the
	// developer, after the three-OS acceptance run (a user gate).
	if Version != "1.0.0-rc.3" {
		t.Fatalf("Version = %q, want 1.0.0-rc.3", Version)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-rc\.\d+)?$`).MatchString(Version) {
		t.Fatalf("VERSION %q is not semver", Version)
	}
}

func TestManifestCarriesTheSameVersion(t *testing.T) {
	b, err := os.ReadFile("../../herdr-plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Version string `toml:"version"`
	}
	if _, err := toml.Decode(string(b), &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != Version {
		t.Fatalf("herdr-plugin.toml says %q, VERSION says %q", m.Version, Version)
	}
}

func TestStringNamesVersionSourceAndCommit(t *testing.T) {
	old := Commit
	t.Cleanup(func() { Commit = old })
	Commit = "1a2b3c4d5e6f"
	if got := String(); got != "herdr-dots "+Version+" (source, commit 1a2b3c4d5e6f)" {
		t.Fatalf("String() = %q", got)
	}
	Commit = ""
	if got := CommitID(); got == "" || strings.ContainsAny(got, " \n") {
		t.Fatalf("CommitID() = %q", got)
	}
}
