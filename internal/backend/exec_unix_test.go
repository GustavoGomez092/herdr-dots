//go:build unix

package backend

import (
	"context"
	"strings"
	"testing"
	"time"
)

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
