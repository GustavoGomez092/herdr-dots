package backend

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCleanEnvStripsHerdrAndClaudeVars(t *testing.T) {
	in := []string{"PATH=/bin", "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH=/s", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_CONFIG_DIR=/c", "HOME=/h"}
	got := CleanEnv(in)
	if !slices.Equal(got, []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/c", "HOME=/h"}) {
		t.Fatalf("CleanEnv = %v", got)
	}
}

func TestCleanEnvNeverReturnsNil(t *testing.T) {
	for _, in := range [][]string{{"HERDR_X=1"}, nil, {}} {
		got := CleanEnv(in)
		if got == nil || len(got) != 0 {
			t.Fatalf("CleanEnv(%v) = %#v, want non-nil empty (nil Env would inherit the parent environment)", in, got)
		}
	}
}

func TestExecRunnerPassesStdinDirEnv(t *testing.T) {
	dir := t.TempDir()
	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", `printf '%s|%s|' "$(pwd)" "$DOTS_X"; cat`},
		Dir: dir, Env: []string{"DOTS_X=yes", "PATH=/usr/bin:/bin"}, Stdin: "from-stdin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "|yes|from-stdin") || !strings.Contains(string(out), dir[len(dir)-8:]) {
		t.Fatalf("out = %q", out)
	}
}

func TestExecRunnerKillsProcessGroupOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := ExecRunner{}.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", "sleep 30 & sleep 30"}, Env: []string{"PATH=/usr/bin:/bin"}})
	if err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v; process group not killed", time.Since(start))
	}
}

func TestHint(t *testing.T) {
	if h := Hint("claude-cli", "Error: Not logged in. Please run /login"); !strings.Contains(h, "claude") || !strings.Contains(h, "/login") {
		t.Fatalf("claude hint %q", h)
	}
	if h := Hint("codex-cli", "401 Unauthorized"); !strings.Contains(h, "codex login") {
		t.Fatalf("codex hint %q", h)
	}
	long := strings.Repeat("e", 1000)
	if h := Hint("claude-cli", long); len(h) > 310 {
		t.Fatalf("hint not truncated: %d", len(h))
	}
}
