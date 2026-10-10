package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The release flow (controller ruling P10-R12): internal/version/VERSION is
// the exact version the binaries report. A candidate is a manual
// workflow_dispatch run of release.yml on main (no tag): it builds the
// archives and opens a pull request that commits release/SHA256SUMS and
// release/ASSET_ENV. Publishing is the tag v<VERSION> pushed on that pull
// request's merge commit. release-kind.sh is the check both steps run.

// releaseKind runs scripts/release-kind.sh with args and returns its trimmed
// output and whether it succeeded.
func releaseKind(t *testing.T, args ...string) (string, bool) {
	t.Helper()
	skipOnWindows(t)
	out, err := exec.Command("sh", append([]string{"release-kind.sh"}, args...)...).Output()
	return strings.TrimSpace(string(out)), err == nil
}

// candidateRun writes what release.yml's candidate job commits for version
// into a fresh release/ directory: ASSET_ENV (the "pin the build inputs"
// step) and SHA256SUMS (GoReleaser's dist/SHA256SUMS for the six archives).
func candidateRun(t *testing.T, version string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "release")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := "HERDR_DOTS_VERSION=" + version + "\nASSET_COMMIT=0123456789ab\nASSET_TIME=1791072000\n"
	var sums strings.Builder
	for _, p := range []string{"darwin_amd64.tar.gz", "darwin_arm64.tar.gz", "linux_amd64.tar.gz", "linux_arm64.tar.gz", "windows_amd64.zip", "windows_arm64.zip"} {
		sums.WriteString(strings.Repeat("a", 64) + "  herdr-dots_" + version + "_" + p + "\n")
	}
	for name, body := range map[string]string{"ASSET_ENV": env, "SHA256SUMS": sums.String()} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The whole sequence: VERSION 1.0.0-rc.1 → candidate dispatch → merge → tag
// v1.0.0-rc.1 publishes a prerelease; later VERSION 1.0.0 → candidate → tag
// v1.0.0 publishes the final release.
func TestReleaseSequenceFromCandidateToFinal(t *testing.T) {
	for _, step := range []struct{ version, want string }{
		{"1.0.0-rc.1", "prerelease"},
		{"1.0.0-rc.2", "prerelease"},
		{"1.0.0", "final"},
	} {
		if got, ok := releaseKind(t, "candidate", step.version); !ok || got != step.want {
			t.Fatalf("candidate dispatch with VERSION %s: got %q ok=%v, want %q", step.version, got, ok, step.want)
		}
		merged := candidateRun(t, step.version)
		if got, ok := releaseKind(t, "publish", step.version, "v"+step.version, merged); !ok || got != step.want {
			t.Fatalf("tag v%s on the merged checksum commit: got %q ok=%v, want %q", step.version, got, ok, step.want)
		}
	}
}

// A tag that is not exactly v<VERSION> is refused.
func TestReleaseTagMustEqualVersion(t *testing.T) {
	merged := candidateRun(t, "1.0.0-rc.1")
	for _, tag := range []string{"v1.0.0", "v1.0.0-rc.2", "1.0.0-rc.1", "v1.0.1", "v1.0.0-rc.1-x", ""} {
		if got, ok := releaseKind(t, "publish", "1.0.0-rc.1", tag, merged); ok {
			t.Errorf("tag %q with VERSION 1.0.0-rc.1 was accepted (%q)", tag, got)
		}
	}
}

// The final review's Critical: VERSION 1.0.0 tagged v1.0.0 on a tree whose
// committed checksums are the candidate's of another version is refused, and
// so is a missing or doubled pin.
func TestReleaseAssetEnvMustBeForThisVersion(t *testing.T) {
	rcTree := candidateRun(t, "1.0.0-rc.1")
	if got, ok := releaseKind(t, "publish", "1.0.0", "v1.0.0", rcTree); ok {
		t.Fatalf("v1.0.0 on the rc.1 checksum commit was accepted (%q)", got)
	}
	// ASSET_ENV right, SHA256SUMS from another version.
	mixed := candidateRun(t, "1.0.0")
	other := candidateRun(t, "1.0.0-rc.1")
	sums, _ := os.ReadFile(filepath.Join(other, "SHA256SUMS"))
	if err := os.WriteFile(filepath.Join(mixed, "SHA256SUMS"), sums, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := releaseKind(t, "publish", "1.0.0", "v1.0.0", mixed); ok {
		t.Fatalf("SHA256SUMS of another version was accepted (%q)", got)
	}
	for name, body := range map[string]string{
		"empty ASSET_ENV":   "",
		"doubled version":   "HERDR_DOTS_VERSION=1.0.0\nHERDR_DOTS_VERSION=1.0.0-rc.1\n",
		"prefix only":       "HERDR_DOTS_VERSION=1.0.0-rc.10\n",
		"regexp look-alike": "HERDR_DOTS_VERSION=1x0x0\n",
	} {
		dir := candidateRun(t, "1.0.0")
		if err := os.WriteFile(filepath.Join(dir, "ASSET_ENV"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, ok := releaseKind(t, "publish", "1.0.0", "v1.0.0", dir); ok {
			t.Errorf("%s was accepted (%q)", name, got)
		}
	}
	empty := candidateRun(t, "1.0.0")
	if err := os.WriteFile(filepath.Join(empty, "SHA256SUMS"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := releaseKind(t, "publish", "1.0.0", "v1.0.0", empty); ok {
		t.Fatalf("an empty SHA256SUMS was accepted (%q)", got)
	}
	if got, ok := releaseKind(t, "publish", "1.0.0", "v1.0.0", filepath.Join(t.TempDir(), "missing")); ok {
		t.Fatalf("a missing release directory was accepted (%q)", got)
	}
}

// Only X.Y.Z and X.Y.Z-rc.<digits> are releases (reviewer Minor 4).
func TestReleaseVersionSuffixMustBeExactlyRC(t *testing.T) {
	for _, v := range []string{"1.0.0-rc.1x", "1.0.0-beta-rc.1", "1.0.0-beta.1", "1.0.0-rc.", "1.0.0-rc1", "1.0.0-RC.1",
		"1.0", "v1.0.0", "1.0.0.1", "1.0.0+build", "", "1.0.0-rc.1\n1.0.0"} {
		if got, ok := releaseKind(t, "candidate", v); ok {
			t.Errorf("candidate VERSION %q was accepted (%q)", v, got)
		}
		dir := candidateRun(t, "1.0.0")
		if got, ok := releaseKind(t, "publish", v, "v"+v, dir); ok {
			t.Errorf("publish VERSION %q was accepted (%q)", v, got)
		}
	}
	if got, ok := releaseKind(t, "tag", "1.0.0"); ok {
		t.Errorf("an unknown step was accepted (%q)", got)
	}
}

// release.yml runs the two checks, publishes an -rc.N version as a
// prerelease, and its checksum pull request names the same tag as the
// ready-to-publish checklist.
func TestReleaseWorkflowAndChecklistAgree(t *testing.T) {
	wf, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile("../docs/release/ready-to-publish.md")
	if err != nil {
		t.Fatal(err)
	}
	yml, md := string(wf), string(doc)
	for _, want := range []string{
		"workflow_dispatch:",
		`tags: ["v*"]`,
		`sh scripts/release-kind.sh candidate "$version"`,
		`sh scripts/release-kind.sh publish "$version" "$GITHUB_REF_NAME" release`,
		`echo "HERDR_DOTS_VERSION=${{ needs.meta.outputs.version }}"`,
		"--prerelease",
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("release.yml lacks %q", want)
		}
	}
	const phrase = "Merge this, then tag `v%s` on the merge commit"
	prBody := strings.Replace(phrase, "%s", "${{ needs.meta.outputs.version }}", 1)
	if !strings.Contains(yml, prBody) {
		t.Errorf("the checksum pull request body lacks %q", prBody)
	}
	if want := strings.Replace(phrase, "%s", "<VERSION>", 1); !strings.Contains(md, want) {
		t.Errorf("ready-to-publish.md lacks %q, the checksum pull request's instruction", want)
	}
	// The checklist's publishing steps walk the same sequence.
	for _, want := range []string{"`v1.0.0-rc.1`", "`v1.0.0`", "prerelease", "Run workflow"} {
		if !strings.Contains(md, want) {
			t.Errorf("ready-to-publish.md lacks %q", want)
		}
	}
}
