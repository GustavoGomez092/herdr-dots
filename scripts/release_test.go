package scripts

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLocalReleaseInstallsEndToEnd builds the real release archives with
// release-local.sh, serves dist/ like a GitHub release and installs the
// host's binary with build.sh. It takes about a minute, so it runs only with
// HERDR_DOTS_E2E_RELEASE=1 (Task 13 runs it; CI's release job runs the
// GoReleaser equivalent).
func TestLocalReleaseInstallsEndToEnd(t *testing.T) {
	skipOnWindows(t)
	if os.Getenv("HERDR_DOTS_E2E_RELEASE") != "1" {
		t.Skip("set HERDR_DOTS_E2E_RELEASE=1 to build and install the real release archives")
	}
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("sh", "scripts/release-local.sh")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("release-local.sh: %v\n%s", err, out)
	}
	dist := filepath.Join(repoRoot, "dist")
	sums, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	want := 6
	if tg := os.Getenv("HERDR_DOTS_TARGETS"); tg != "" {
		want = len(strings.Fields(tg))
	}
	if n := strings.Count(string(sums), "\n"); n != want {
		t.Fatalf("SHA256SUMS lists %d archives, want %d:\n%s", n, want, sums)
	}
	version, err := os.ReadFile(filepath.Join(repoRoot, "internal", "version", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	ver := strings.TrimSpace(string(version))
	prefix := "/" + testRepo + "/releases/download/v" + ver + "/"
	srv := httptest.NewServer(http.StripPrefix(prefix, http.FileServer(http.Dir(dist))))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	for rel, body := range map[string]string{
		"internal/version/VERSION": ver + "\n",
		"release/REPO":             testRepo + "\n",
		"release/SHA256SUMS":       string(sums),
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script, _ := os.ReadFile("build.sh")
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.sh"), script, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "scripts/build.sh")
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + toolDir(t, false), "HOME=" + t.TempDir(), "HERDR_DOTS_RELEASE_BASE=" + srv.URL,
		"HERDR_DOTS_PLATFORM=" + runtime.GOOS + "_" + runtime.GOARCH}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build.sh: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(root, "bin", "herdr-dots"), "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); !strings.HasPrefix(got, "herdr-dots "+ver+" (release, commit ") {
		t.Fatalf("--version = %q", got)
	}
}

// The archive names GoReleaser makes are the names the install scripts and
// release-local.sh look for.
func TestGoReleaserConfigMatchesTheInstallScripts(t *testing.T) {
	b, err := os.ReadFile("../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	for _, want := range []string{
		"project_name: herdr-dots",
		`name_template: "{{ .ProjectName }}_{{ .Env.HERDR_DOTS_VERSION }}_{{ .Os }}_{{ .Arch }}"`,
		"goos: [darwin, linux, windows]",
		"goarch: [amd64, arm64]",
		"formats: [zip]",
		"name_template: SHA256SUMS",
		"-X herdr-dots/internal/version.Source=release",
		"-buildvcs=false",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf(".goreleaser.yaml lacks %q", want)
		}
	}
	for _, f := range []string{"build.sh", "release-local.sh"} {
		s, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(s), "${NAME}_${VERSION}_") {
			t.Errorf("%s does not name archives <name>_<version>_<os>_<arch>", f)
		}
	}
}
