//go:build windows

package platform

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsConstants(t *testing.T) {
	if DevNull != "NUL" || EnvKey("Herdr_Pane_Id") != "HERDR_PANE_ID" || HomeEnv != "USERPROFILE" {
		t.Fatalf("DevNull %q; EnvKey must fold case on Windows", DevNull)
	}
}

// ChildEnv starts from the process environment when given none and always
// stops cmd.exe searching the current directory.
func TestChildEnvTurnsOffTheCurrentDirectorySearch(t *testing.T) {
	t.Setenv("DOTS_CHILDENV_PROBE", "kept")
	got := ChildEnv(nil)
	if !slices.Contains(got, "DOTS_CHILDENV_PROBE=kept") || !slices.Contains(got, NoCwdExeSearch+"=1") {
		t.Fatalf("ChildEnv(nil) = %q", got)
	}
	got = ChildEnv([]string{"A=b", "NODEFAULTCURRENTDIRECTORYINEXEPATH=0"})
	if !slices.Equal(got, []string{"A=b", NoCwdExeSearch + "=1"}) {
		t.Fatalf("ChildEnv = %q", got)
	}
}

// EditorCommand's environment goes through ChildEnv too.
func TestEditorCommandEnvHasNoCwdExeSearch(t *testing.T) {
	cmd := EditorCommand("notepad", filepath.Join(t.TempDir(), "m.toml"))
	n := 0
	for _, kv := range cmd.Env {
		if name, _, _ := strings.Cut(kv, "="); strings.EqualFold(name, NoCwdExeSearch) {
			n++
		}
	}
	if n != 1 || !slices.Contains(cmd.Env, NoCwdExeSearch+"=1") {
		t.Fatalf("editor env holds %d copies: %q", n, cmd.Env)
	}
}

func TestLookPathPrefersExeOverAnEarlierCmdShim(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(a, "claude.cmd"), []byte("@echo off\r\n"), 0o700)
	os.WriteFile(filepath.Join(b, "claude.exe"), []byte("MZ"), 0o700)
	t.Setenv("PATH", a+string(os.PathListSeparator)+b)
	got, err := LookPath("claude")
	if err != nil || !strings.EqualFold(got, filepath.Join(b, "claude.exe")) {
		t.Fatalf("LookPath = %q, %v", got, err)
	}
	os.Remove(filepath.Join(b, "claude.exe"))
	got, err = LookPath("claude")
	if err != nil || !strings.EqualFold(got, filepath.Join(a, "claude.cmd")) {
		t.Fatalf("without an .exe LookPath = %q, %v", got, err)
	}
}

// echoShim puts echo-args.cmd on PATH; it prints its arguments in brackets.
func echoShim(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "echo-args.cmd"), []byte("@echo off\r\necho [%*]\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCommandPassesShimArgumentsLiterally(t *testing.T) {
	echoShim(t)
	cmd, err := Command("echo-args", []string{"a&b", "c d", "x & echo INJECTED", "p|q", ""})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != `["a&b" "c d" "x & echo INJECTED" "p|q" ""]` {
		t.Fatalf("shim saw %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) == "INJECTED" {
			t.Fatal("cmd.exe ran an injected command")
		}
	}
}

func TestCommandRefusesUnsafeShimArguments(t *testing.T) {
	echoShim(t)
	if _, err := Command("echo-args", []string{"100%"}); !errors.Is(err, ErrUnsafeBatchArg) {
		t.Fatalf("err = %v, want ErrUnsafeBatchArg", err)
	}
	if _, err := Command("echo-args", []string{strings.Repeat("y", 9000)}); !errors.Is(err, ErrCmdTooLong) {
		t.Fatalf("err = %v, want ErrCmdTooLong", err)
	}
}

// pingTree starts cmd.exe running ping as a grandchild that shares stdout, so
// Wait only returns once the grandchild is gone too.
func pingTree(t *testing.T) (*exec.Cmd, *Tree) {
	t.Helper()
	cmd := exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1")
	var out bytes.Buffer
	cmd.Stdout = &out
	tree, err := StartTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // let cmd.exe start ping
	return cmd, tree
}

func TestTreeKillEndsGrandchildren(t *testing.T) {
	cmd, tree := pingTree(t)
	defer tree.Close()
	start := time.Now()
	if err := tree.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("process tree took %v to die", d)
	}
}

func TestTreeCloseKillsWhatIsLeft(t *testing.T) {
	cmd, tree := pingTree(t)
	start := time.Now()
	if err := tree.Close(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("KILL_ON_JOB_CLOSE took %v", d)
	}
}

// TestDetachHelper is the parent process for the test below: it starts a
// detached ping, prints its pid and exits at once.
func TestDetachHelper(t *testing.T) {
	if os.Getenv("PLATFORM_DETACH_HELPER") != "1" {
		t.Skip("helper process for TestStartDetachedSurvivesItsParent")
	}
	p, err := StartDetached(exec.Command("ping", "-n", "30", "127.0.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("PID " + strconv.Itoa(p.Pid) + "\n")
	_ = p.Release()
}

func TestStartDetachedSurvivesItsParent(t *testing.T) {
	helper := exec.Command(os.Args[0], "-test.run=^TestDetachHelper$", "-test.count=1", "-test.v")
	helper.Env = append(os.Environ(), "PLATFORM_DETACH_HELPER=1")
	out, err := helper.Output()
	if err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	pid := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PID "); ok {
			pid, _ = strconv.Atoi(v)
		}
	}
	if pid == 0 {
		t.Fatalf("no pid in helper output:\n%s", out)
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatalf("detached process %d is gone: %v", pid, err)
	}
	defer windows.CloseHandle(h)
	defer windows.TerminateProcess(h, 1)
	if ev, _ := windows.WaitForSingleObject(h, 0); ev != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("detached process %d exited with its parent", pid)
	}
}

func TestCommandContextRunsAShimUnderTheContext(t *testing.T) {
	echoShim(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd, err := CommandContext(ctx, "echo-args", []string{"x y"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != `["x y"]` {
		t.Fatalf("%q %v", out, err)
	}
}
