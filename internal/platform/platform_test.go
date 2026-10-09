package platform

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestHerdrConfigPathOverridesThenDefault(t *testing.T) {
	home := filepath.Join(t.TempDir(), "José Núñez")
	if got := HerdrConfigPath(envMap(map[string]string{"HERDR_CONFIG_PATH": "/custom.toml", "XDG_CONFIG_HOME": "/x"}), home); got != "/custom.toml" {
		t.Fatalf("HERDR_CONFIG_PATH must win: %s", got)
	}
	if got := HerdrConfigPath(envMap(map[string]string{"XDG_CONFIG_HOME": "/x"}), home); got != filepath.Join("/x", "herdr", "config.toml") {
		t.Fatalf("XDG_CONFIG_HOME: %s", got)
	}
	got := HerdrConfigPath(envMap(nil), home)
	if !strings.HasSuffix(got, filepath.Join("herdr", "config.toml")) || !strings.HasPrefix(got, home) {
		t.Fatalf("default %s not under home %s", got, home)
	}
}

func TestWindowsHerdrConfigDirRules(t *testing.T) {
	home := filepath.Join("Users", "José Núñez")
	appdata := filepath.Join("Users", "José Núñez", "AppData", "Roaming")
	if got := windowsHerdrConfigDir(envMap(map[string]string{"APPDATA": appdata, "USERPROFILE": "/ignored"}), home); got != filepath.Join(appdata, "herdr") {
		t.Fatalf("APPDATA rule: %s", got)
	}
	if got := windowsHerdrConfigDir(envMap(map[string]string{"USERPROFILE": home}), "/other"); got != filepath.Join(home, "AppData", "Roaming", "herdr") {
		t.Fatalf("USERPROFILE rule: %s", got)
	}
	if got := windowsHerdrConfigDir(envMap(nil), home); got != filepath.Join(home, ".config", "herdr") {
		t.Fatalf("home fallback: %s", got)
	}
}

func TestClientPaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), "my home")
	for got, want := range map[string]string{
		ClaudeProjectsDir(home): filepath.Join(home, ".claude", "projects"),
		CodexSessionsDir(home):  filepath.Join(home, ".codex", "sessions"),
		ClaudeUserConfig(home):  filepath.Join(home, ".claude.json"),
		CodexConfig(home):       filepath.Join(home, ".codex", "config.toml"),
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestIsBatchFile(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\Users\x\AppData\Roaming\npm\claude.cmd`: true,
		`C:\tools\CODEX.CMD`:                        true,
		`C:\tools\run.bat`:                          true,
		`C:\Users\x\.local\bin\claude.exe`:          false,
		"/usr/local/bin/claude":                     false,
		`C:\dir.cmd\claude.exe`:                     false,
	} {
		if IsBatchFile(p) != want {
			t.Errorf("IsBatchFile(%q) = %v", p, !want)
		}
	}
}

func TestFindExecutablePrefersFirstAbsoluteDirWithTheFile(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(b, "claude.exe"), nil, 0o700); err != nil {
		t.Fatal(err)
	}
	isFile := func(p string) bool { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }
	if got := findExecutable("claude", []string{"", "relative", a, b}, ".exe", isFile); got != filepath.Join(b, "claude.exe") {
		t.Fatalf("got %q", got)
	}
	if got := findExecutable("codex", []string{a, b}, ".exe", isFile); got != "" {
		t.Fatalf("missing program found: %q", got)
	}
	wd, _ := os.Getwd()
	if rel, err := filepath.Rel(wd, b); err == nil {
		if got := findExecutable("claude", []string{rel, a}, ".exe", isFile); got != "" {
			t.Fatalf("relative PATH entry %q must be skipped, got %q", rel, got)
		}
	}
}

func TestBatchCmdLineQuotesEveryArgument(t *testing.T) {
	line, err := BatchCmdLine(`C:\Windows\system32\cmd.exe`, `C:\Users\José Núñez\AppData\Roaming\npm\claude.cmd`,
		[]string{"-p", "--tools", "", "--settings", `C:\Users\José Núñez\AppData\Local\Temp\herdr-dots-run-1\settings.json`, "a&b|c<d>e^f(g)", `C:\repo\`})
	if err != nil {
		t.Fatal(err)
	}
	want := `C:\Windows\system32\cmd.exe /d /s /c ""C:\Users\José Núñez\AppData\Roaming\npm\claude.cmd" "-p" "--tools" "" "--settings" "C:\Users\José Núñez\AppData\Local\Temp\herdr-dots-run-1\settings.json" "a&b|c<d>e^f(g)" "C:\repo\\""`
	if line != want {
		t.Fatalf("line\n got %s\nwant %s", line, want)
	}
}

func TestBatchCmdLineRefusesCharactersCmdCantCarry(t *testing.T) {
	for _, bad := range []string{`say "hi"`, "100%", "%PATH%", "wow!", "two\nlines", "cr\r"} {
		if _, err := BatchCmdLine("cmd.exe", `C:\npm\claude.cmd`, []string{"-p", bad}); !errors.Is(err, ErrUnsafeBatchArg) {
			t.Errorf("%q: err = %v, want ErrUnsafeBatchArg", bad, err)
		}
	}
}

// The refusal names the argument's position and the character class, never
// the argument itself: it may be a prompt or hold a key.
func TestBatchCmdLineRefusalNeverEchoesTheArgument(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-p", "x", `sk-SECRET"quoted`}, "argument 3 contains a double quote"},
		{[]string{"sk-SECRET%PATH%"}, "argument 1 contains a percent sign"},
		{[]string{"-p", "sk-SECRET!"}, "argument 2 contains an exclamation mark"},
		{[]string{"sk-SECRET\nline"}, "argument 1 contains a line break"},
		{[]string{"sk-SECRET\x00"}, "argument 1 contains a NUL character"},
	} {
		_, err := BatchCmdLine("cmd.exe", `C:\npm\claude.cmd`, c.args)
		if !errors.Is(err, ErrUnsafeBatchArg) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want %q", c.args, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%q: the error echoes the argument: %v", c.args, err)
		}
	}
	_, err := BatchCmdLine("cmd.exe", `C:\100% SECRET\claude.cmd`, nil)
	if !errors.Is(err, ErrUnsafeBatchArg) || !strings.Contains(err.Error(), "the program path contains a percent sign") || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("script path: %v", err)
	}
}

func TestBatchCmdLineRefusesOverLongLinesWithAHint(t *testing.T) {
	_, err := BatchCmdLine("cmd.exe", `C:\npm\claude.cmd`, []string{strings.Repeat("x", 8200)})
	if !errors.Is(err, ErrCmdTooLong) || !strings.Contains(err.Error(), "claude.cmd") || !strings.Contains(err.Error(), "claude.exe") || !strings.Contains(err.Error(), "> 8191") {
		t.Fatalf("err = %v", err)
	}
	if _, err := BatchCmdLine("cmd.exe", `C:\npm\claude.cmd`, []string{strings.Repeat("x", 8000)}); err != nil {
		t.Fatalf("8000 characters fit: %v", err)
	}
}

func TestConnRefusedRecognisesAClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, err = net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil || !ConnRefused(err) {
		t.Fatalf("dial a closed port: %v (ConnRefused=%v)", err, ConnRefused(err))
	}
	if ConnRefused(errors.New("timeout")) {
		t.Fatal("other errors are not refusals")
	}
	if !ConnRefused(fmt.Errorf("kept: %w", ErrConnRefused)) {
		t.Fatal("ErrConnRefused must count as a refusal")
	}
}
