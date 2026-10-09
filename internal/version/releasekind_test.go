package version

import (
	"os/exec"
	"strings"
	"testing"
)

// TestReleaseKindClassifiesTags runs scripts/release-kind.sh, the classifier
// release.yml calls (controller ruling P10-R1): a tag equal to v$VERSION is a
// candidate when VERSION ends in -rc.N and a final publish otherwise;
// anything else fails.
func TestReleaseKindClassifiesTags(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	cases := []struct {
		version, tag, want string
		ok                 bool
	}{
		{"1.0.0-rc.1", "v1.0.0-rc.1", "candidate", true},
		{"1.0.0-rc.2", "v1.0.0-rc.2", "candidate", true},
		{"1.0.0", "v1.0.0", "final", true},
		{"1.0.0-rc.1", "v1.0.0", "", false},
		{"1.0.0", "v1.0.0-rc.1", "", false},
		{"1.0.0-rc.1", "v1.0.0-rc.2", "", false},
		{"1.0.0-rc.1", "1.0.0-rc.1", "", false},
		{"1.0.0-rc.1", "v1.0.1", "", false},
		{"1.0.0-beta.1", "v1.0.0-beta.1", "", false},
	}
	for _, c := range cases {
		out, err := exec.Command(bash, "../../scripts/release-kind.sh", c.version, c.tag).Output()
		got := strings.TrimSpace(string(out))
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("VERSION %s tag %s: got %q err %v, want %q", c.version, c.tag, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("VERSION %s tag %s: want failure, got %q", c.version, c.tag, got)
		}
	}
}
