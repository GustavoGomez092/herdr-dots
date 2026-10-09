//go:build windows

package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform"
)

func TestExecRunnerPassesStdinDirEnvOnWindows(t *testing.T) {
	dir := t.TempDir()
	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{
		Name: "cmd.exe", Args: []string{"/d", "/c", "cd & echo %DOTS_X% & sort"},
		Dir: dir, Env: []string{"DOTS_X=yes", "PATH=" + os.Getenv("PATH")}, Stdin: "from-stdin\r\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "yes") || !strings.Contains(s, "from-stdin") || !strings.Contains(strings.ToLower(s), strings.ToLower(filepath.Base(dir))) {
		t.Fatalf("out = %q", s)
	}
}

func TestExecRunnerKillsTheJobOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := ExecRunner{}.Run(ctx, Cmd{Name: "cmd.exe", Args: []string{"/d", "/c", "ping -n 30 127.0.0.1"}, Env: []string{"PATH=" + os.Getenv("PATH")}})
	if err == nil {
		t.Fatal("want timeout error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v; the ping grandchild survived the timeout", d)
	}
}

func TestExecRunnerRefusesUnsafeShimArgsBeforeStarting(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fakecli.cmd"), []byte("@echo off\r\necho ran\r\n"), 0o700)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{Name: "fakecli", Args: []string{"--model", "50%"}, Env: os.Environ()})
	if !errors.Is(err, platform.ErrUnsafeBatchArg) || len(out) != 0 {
		t.Fatalf("err = %v out = %q", err, out)
	}
}
