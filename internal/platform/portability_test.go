package platform

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unixOnly maps a source pattern that only works on Unix (or hides an OS
// branch) to what portable code uses instead.
var unixOnly = map[string]string{
	`"/dev/null"`:          "platform.DevNull",
	"syscall.Kill(":        "platform.StartTree / Tree.Kill",
	"Setpgid":              "platform.StartTree",
	"Setsid":               "platform.StartDetached",
	"syscall.Flock":        "internal/lockfile",
	"syscall.O_NOFOLLOW":   "platform.OpenNoFollow",
	"syscall.Stat_t":       "platform.CheckPrivate",
	"syscall.ECONNREFUSED": "platform.ConnRefused",
	`"unix"`:               "ipc.Listen / ipc.Dial (build-tagged)",
	"os.Getuid()":          "platform.CheckPrivate",
	"exec.LookPath(":       "platform.LookPath (prefers .exe over .cmd shims)",
	"runtime.GOOS":         "a _unix.go / _windows.go file or internal/platform",
	".EngineSock()":        "Paths.EngineEndpoint",
	`"/tmp`:                "os.TempDir()",
	`".claude"`:            "platform.ClaudeProjectsDir / ClaudeUserConfig",
	`".claude.json"`:       "platform.ClaudeUserConfig",
	`".codex"`:             "platform.CodexSessionsDir / CodexConfig",
	`Command("claude"`:     "platform.Command (safe for a claude.cmd shim)",
	`Command("codex"`:      "platform.Command (safe for a codex.cmd shim)",
	`(ctx, "claude"`:       "platform.Command (safe for a claude.cmd shim)",
	`(ctx, "codex"`:        "platform.Command (safe for a codex.cmd shim)",
	`exec.Command("sh"`:    "platform.EditorCommand (no sh on Windows)",
	`"HOME"`:               "platform.HomeEnv or os.UserHomeDir (Windows reads USERPROFILE)",
}

// moduleRoot walks up from the test's directory to go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// osSpecific reports whether a file only builds on some OSes: a _unix.go or
// _windows.go name, or a //go:build line.
func osSpecific(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_unix.go") || strings.HasSuffix(base, "_windows.go") {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "//go:build") {
			return true
		}
		if strings.HasPrefix(line, "package ") {
			return false
		}
	}
	return false
}

// TestProductCodeHasNoUnixOnlyCalls keeps every later phase portable: shared
// (untagged) product code may not use Unix-only calls or branch on the OS.
// internal/platform, OS-specific files and tests are exempt.
func TestProductCodeHasNoUnixOnlyCalls(t *testing.T) {
	root := moduleRoot(t)
	var bad []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && rel != "." || rel == "internal/platform" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || osSpecific(p) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			code, _, _ := strings.Cut(line, "//")
			for pat, use := range unixOnly {
				if strings.Contains(code, pat) {
					bad = append(bad, rel+":"+itoa(i+1)+": "+pat+" (use "+use+")")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Error(b)
	}
}

// The jail's file listing child runs through platform.CommandContext
// (batch-arg-safe for a shim), never through os/exec directly.
func TestToolloopListingChildGoesThroughPlatform(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "backend", "toolloop", "tools.go"))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		code, _, _ := strings.Cut(line, "//")
		if strings.Contains(code, "exec.Command") {
			t.Errorf("internal/backend/toolloop/tools.go:%d: exec.Command (use platform.CommandContext)", i+1)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
