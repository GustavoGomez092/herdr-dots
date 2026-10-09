// Package scripts tests the install scripts Herdr runs as [[build]] steps.
// The tests copy a script into a temp plugin root, serve a fake GitHub
// release from httptest and run the script with a PATH that has exactly
// the tools the case needs.
package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	testVersion = "1.0.0-rc.1"
	testRepo    = "acme/herdr-dots"
	testPlat    = "darwin_arm64"
	testAsset   = "herdr-dots_" + testVersion + "_" + testPlat + ".tar.gz"
	fakeBinary  = "#!/bin/sh\necho fake-release-binary\n"
)

// unixTools are what build.sh needs besides go.
var unixTools = []string{"sh", "uname", "tr", "awk", "sed", "grep", "cut", "mkdir", "mktemp", "mv", "rm", "chmod",
	"cat", "dirname", "tar", "gzip", "curl", "shasum", "sha256sum", "head"}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("build.sh is the macOS and Linux build step; build.ps1 is tested on Windows")
	}
}

// toolDir links the named tools (those that exist here) into a new dir, so
// PATH can include or leave out go.
func toolDir(t *testing.T, withGo bool) string {
	t.Helper()
	dir := t.TempDir()
	names := unixTools
	if withGo {
		names = append(append([]string{}, names...), "go")
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			if err := os.Symlink(p, filepath.Join(dir, n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func tarGz(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// pluginRoot is a temp plugin checkout: the script, VERSION, release/ files
// and a tiny module whose binary prints its version.Source.
func pluginRoot(t *testing.T, sums string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"internal/version/VERSION":    testVersion + "\n",
		"release/REPO":                testRepo + "\n",
		"release/SHA256SUMS":          sums,
		"go.mod":                      "module herdr-dots\n\ngo 1.25\n",
		"internal/version/version.go": "package version\n\nvar Source = \"unset\"\n",
		"cmd/herdr-dots/main.go": "package main\n\nimport (\n\t\"fmt\"\n\n\t\"herdr-dots/internal/version\"\n)\n\n" +
			"func main() { fmt.Println(\"built-from-\" + version.Source) }\n",
	}
	script, err := os.ReadFile("build.sh")
	if err != nil {
		t.Fatal(err)
	}
	files["scripts/build.sh"] = string(script)
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// release serves one asset (and counts requests) the way GitHub does.
func release(t *testing.T, asset []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/"+testRepo+"/releases/download/v"+testVersion+"/"+testAsset {
			_, _ = w.Write(asset)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func goEnv(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// runBuild runs scripts/build.sh in root and returns its stderr.
func runBuild(t *testing.T, root, base string, withGo bool, extra ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "scripts/build.sh")
	cmd.Dir = root
	cmd.Env = append([]string{
		"PATH=" + toolDir(t, withGo),
		"HOME=" + t.TempDir(),
		"HERDR_DOTS_PLATFORM=" + testPlat,
		"HERDR_DOTS_RELEASE_BASE=" + base,
		"GOTOOLCHAIN=local",
		"GOFLAGS=-mod=mod",
		"GOCACHE=" + goEnv(t, "GOCACHE"),
		"GOPATH=" + goEnv(t, "GOPATH"),
	}, extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func runInstalled(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command(filepath.Join(root, "bin", "herdr-dots")).Output()
	if err != nil {
		t.Fatalf("run bin/herdr-dots: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func readInstallSource(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "bin", "INSTALL_SOURCE"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// binLeftovers lists what is in bin/ besides the expected entries.
func binLeftovers(t *testing.T, root string, want ...string) []string {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Join(root, "bin"))
	var extra []string
	for _, e := range ents {
		ok := false
		for _, w := range want {
			ok = ok || e.Name() == w
		}
		if !ok {
			extra = append(extra, e.Name())
		}
	}
	return extra
}

func TestVerifiedReleaseIsInstalled(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "herdr-dots", fakeBinary)
	srv, hits := release(t, asset)
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n%s  herdr-dots_%s_linux_amd64.tar.gz\n", sum(asset), testAsset, strings.Repeat("0", 64), testVersion))
	stderr, err := runBuild(t, root, srv.URL, false)
	if err != nil {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if got := runInstalled(t, root); got != "fake-release-binary" {
		t.Fatalf("installed binary says %q", got)
	}
	src := readInstallSource(t, root)
	for _, want := range []string{"source=release\n", "repo=" + testRepo + "\n", "asset=" + testAsset + "\n", "sha256=" + sum(asset) + "\n"} {
		if !strings.Contains(src, want) {
			t.Errorf("INSTALL_SOURCE lacks %q:\n%s", want, src)
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "bin", "release", testAsset)); err != nil || sum(b) != sum(asset) {
		t.Fatalf("the verified archive is kept for verify-release: %v", err)
	}
	if extra := binLeftovers(t, root, "herdr-dots", "INSTALL_SOURCE", "release"); len(extra) != 0 {
		t.Fatalf("leftovers in bin/: %v", extra)
	}
	if hits.Load() < 1 {
		t.Fatal("no download happened")
	}
}

func TestChecksumMismatchFallsBackToASourceBuild(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "herdr-dots", fakeBinary)
	srv, _ := release(t, asset)
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n", strings.Repeat("ab", 32), testAsset))
	stderr, err := runBuild(t, root, srv.URL, true)
	if err != nil {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "checksum mismatch") {
		t.Fatalf("stderr does not explain the fallback:\n%s", stderr)
	}
	if got := runInstalled(t, root); got != "built-from-source" {
		t.Fatalf("installed binary says %q, want the source build", got)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "release", testAsset)); !os.IsNotExist(err) {
		t.Fatal("the unverified archive must not be kept")
	}
	if !strings.HasPrefix(readInstallSource(t, root), "source=source\n") {
		t.Fatal("INSTALL_SOURCE must say source")
	}
}

func TestNoChecksumMeansNoDownload(t *testing.T) {
	skipOnWindows(t)
	srv, hits := release(t, tarGz(t, "herdr-dots", fakeBinary))
	root := pluginRoot(t, "# filled in by the release workflow\n")
	stderr, err := runBuild(t, root, srv.URL, true)
	if err != nil {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests without a checksum to check them against", hits.Load())
	}
	if got := runInstalled(t, root); got != "built-from-source" {
		t.Fatalf("installed binary says %q", got)
	}
}

func TestArchiveWithoutTheBinaryIsRejected(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "something-else", fakeBinary)
	srv, _ := release(t, asset)
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), testAsset))
	stderr, err := runBuild(t, root, srv.URL, true)
	if err != nil || !strings.Contains(stderr, "has no herdr-dots binary") {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if got := runInstalled(t, root); got != "built-from-source" {
		t.Fatalf("installed binary says %q", got)
	}
}

func TestOfflineWithoutGoFailsWithInstructionsAndAnEmptyBin(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "herdr-dots", fakeBinary)
	srv, hits := release(t, asset)
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), testAsset))
	stderr, err := runBuild(t, root, srv.URL, false, "HERDR_PLUGIN_BUILD_OFFLINE=1")
	if err == nil {
		t.Fatal("an offline install without Go must fail")
	}
	for _, want := range []string{"HERDR_PLUGIN_BUILD_OFFLINE=1", "Install Go 1.25 or newer", "release/SHA256SUMS"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("offline mode downloaded")
	}
	if extra := binLeftovers(t, root); len(extra) != 0 {
		t.Fatalf("bin/ must stay empty: %v", extra)
	}
}

func TestUnreachableServerWithoutGoFailsCleanly(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "herdr-dots", fakeBinary)
	srv, _ := release(t, asset)
	srv.Close() // nothing listens any more
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), testAsset))
	stderr, err := runBuild(t, root, srv.URL, false)
	if err == nil || !strings.Contains(stderr, "download failed") || !strings.Contains(stderr, "Install Go 1.25 or newer") {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if extra := binLeftovers(t, root); len(extra) != 0 {
		t.Fatalf("bin/ must stay empty: %v", extra)
	}
}

func TestScriptsPassShellcheck(t *testing.T) {
	skipOnWindows(t)
	sc, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("shellcheck not installed")
	}
	scripts, _ := filepath.Glob("*.sh")
	out, err := exec.Command(sc, append([]string{"-s", "sh"}, scripts...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("shellcheck: %v\n%s", err, out)
	}
}
