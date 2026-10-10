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

// Review Focus (Critical): npm's claude.cmd/codex.cmd run "node" when no
// node.exe sits beside the shim, and cmd.exe would look in the current
// directory (the pane's project) before PATH. A node.cmd planted in the
// project must never run; the real node on PATH must.
func TestExecRunnerNeverRunsARepoLocalNodeThroughAShim(t *testing.T) {
	for _, env := range []struct {
		name string
		env  func(bin string) []string
	}{
		{"explicit env", func(bin string) []string {
			return []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}
		}},
		{"nil env", func(string) []string { return nil }},
	} {
		t.Run(env.name, func(t *testing.T) {
			bin, repo := t.TempDir(), t.TempDir()
			planted := filepath.Join(repo, "planted-ran.txt")
			// The npm shim pattern: no node.exe beside it, so it runs "node".
			if err := os.WriteFile(filepath.Join(bin, "fakenpmcli.cmd"),
				[]byte("@echo off\r\nSET \"_prog=node\"\r\n\"%_prog%\" %*\r\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			// The real node, on PATH.
			if err := os.WriteFile(filepath.Join(bin, "node.cmd"), []byte("@echo off\r\necho real-node %*\r\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"node.cmd", "node.bat"} {
				if err := os.WriteFile(filepath.Join(repo, name),
					[]byte("@echo off\r\necho planted > \""+planted+"\"\r\necho planted-node\r\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, stderr, err := ExecRunner{}.Run(context.Background(), Cmd{Name: "fakenpmcli", Args: []string{"hi"}, Dir: repo, Env: env.env(bin)})
			if _, serr := os.Stat(planted); serr == nil {
				t.Fatalf("the repo-local node ran (out %q)", out)
			}
			// ExecRunner quotes every shim argument (platform.BatchCmdLine) and
			// the shim's %* passes them on as written: the real node.exe gets
			// argv "hi"; this node.cmd echoes the quoted form.
			if err != nil || !strings.Contains(string(out), `real-node "hi"`) {
				t.Fatalf("out %q stderr %q err %v", out, stderr, err)
			}
		})
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
