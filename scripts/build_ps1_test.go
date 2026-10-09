package scripts

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const winAsset = "herdr-dots_" + testVersion + "_windows_amd64.zip"

// powershell finds pwsh (macOS, Linux, Windows) or Windows PowerShell; the
// tests skip without one (CI's ubuntu and windows runners have pwsh).
func powershell(t *testing.T) string {
	t.Helper()
	for _, n := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	t.Skip("PowerShell is not installed")
	return ""
}

func zipBytes(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func psRoot(t *testing.T, sums string) string {
	t.Helper()
	root := pluginRoot(t, sums)
	script, err := os.ReadFile("build.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.ps1"), script, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runPS(t *testing.T, ps, root, base string, extra ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join("scripts", "build.ps1"))
	cmd.Dir = root
	cmd.Env = append([]string{"PATH=" + filepath.Dir(ps), "HOME=" + t.TempDir(), "USERPROFILE=" + t.TempDir(),
		"SystemRoot=" + os.Getenv("SystemRoot"), "HERDR_DOTS_PLATFORM=windows_amd64", "HERDR_DOTS_RELEASE_BASE=" + base}, extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func TestPowerShellInstallsAVerifiedRelease(t *testing.T) {
	ps := powershell(t)
	asset := zipBytes(t, "herdr-dots.exe", "fake windows binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+testRepo+"/releases/download/v"+testVersion+"/"+winAsset {
			_, _ = w.Write(asset)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	root := psRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), winAsset))
	stderr, err := runPS(t, ps, root, srv.URL)
	if err != nil {
		t.Fatalf("build.ps1: %v\n%s", err, stderr)
	}
	if b, err := os.ReadFile(filepath.Join(root, "bin", "herdr-dots.exe")); err != nil || string(b) != "fake windows binary" {
		t.Fatalf("installed binary: %q %v", b, err)
	}
	if src := readInstallSource(t, root); !strings.Contains(src, "source=release\n") || !strings.Contains(src, "asset="+winAsset+"\n") {
		t.Fatalf("INSTALL_SOURCE:\n%s", src)
	}
}

func TestPowerShellRefusesAMismatchAndFailsWithoutGo(t *testing.T) {
	ps := powershell(t)
	asset := zipBytes(t, "herdr-dots.exe", "fake windows binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(asset) }))
	t.Cleanup(srv.Close)
	root := psRoot(t, fmt.Sprintf("%s  %s\n", strings.Repeat("ab", 32), winAsset))
	stderr, err := runPS(t, ps, root, srv.URL)
	if err == nil {
		t.Fatal("a checksum mismatch without Go must fail")
	}
	for _, want := range []string{"checksum mismatch", "Install Go 1.25 or newer"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if extra := binLeftovers(t, root); len(extra) != 0 {
		t.Fatalf("bin/ must stay empty: %v", extra)
	}
}

// An empty release/REPO (the state until the user fills it in) must fall
// back cleanly, not crash on a $null .Trim().
func TestPowerShellEmptyRepoFallsBack(t *testing.T) {
	ps := powershell(t)
	asset := zipBytes(t, "herdr-dots.exe", "fake windows binary")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); _, _ = w.Write(asset) }))
	t.Cleanup(srv.Close)
	root := psRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), winAsset))
	if err := os.WriteFile(filepath.Join(root, "release", "REPO"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stderr, _ := runPS(t, ps, root, srv.URL)
	if !strings.Contains(stderr, "unknown GitHub repository") || strings.Contains(stderr, "null-valued") {
		t.Fatalf("empty REPO:\n%s", stderr)
	}
	if hits.Load() != 0 {
		t.Fatal("downloaded without a repository")
	}
	if b, err := os.ReadFile(filepath.Join(root, "bin", "herdr-dots.exe")); err == nil && string(b) == "fake windows binary" {
		t.Fatal("installed a release without a repository")
	}
}

// A bad HERDR_DOTS_PLATFORM never becomes part of a download URL.
func TestPowerShellRefusesAnInvalidPlatform(t *testing.T) {
	ps := powershell(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); http.NotFound(w, r) }))
	t.Cleanup(srv.Close)
	root := psRoot(t, fmt.Sprintf("%s  %s\n", strings.Repeat("ab", 32), winAsset))
	stderr, _ := runPS(t, ps, root, srv.URL, "HERDR_DOTS_PLATFORM=../../evil")
	if !strings.Contains(stderr, "HERDR_DOTS_PLATFORM") || hits.Load() != 0 {
		t.Fatalf("invalid platform (%d requests):\n%s", hits.Load(), stderr)
	}
}
