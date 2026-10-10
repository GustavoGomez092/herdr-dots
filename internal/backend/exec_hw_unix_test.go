//go:build unix

package backend

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// S6 (post-rc hardening): ExecRunner applies CleanEnv itself, so a caller
// that forgets it (or passes no Env) still never hands Herdr's variables or
// the Claude session markers to a dot's CLI.
func TestExecRunnerCleansTheEnvironmentItself(t *testing.T) {
	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", `printf '%s|%s|%s' "$HERDR_PANE_ID" "$CLAUDECODE" "$DOTS_KEEP"`},
		Env: []string{"HERDR_PANE_ID=w1:p1", "CLAUDECODE=1", "DOTS_KEEP=kept", "PATH=/usr/bin:/bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "||kept" {
		t.Fatalf("child saw %q, want only DOTS_KEEP", out)
	}
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/herdr.sock")
	out, _, err = ExecRunner{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", `printf '%s' "$HERDR_SOCKET_PATH"`}})
	if err != nil || string(out) != "" {
		t.Fatalf("with no Env the parent's HERDR_* must still be dropped: %q %v", out, err)
	}
}

// B5 (post-rc hardening): a run whose context is already cancelled starts
// nothing.
func TestExecRunnerStartsNothingWhenAlreadyCancelled(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := ExecRunner{}.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", "touch " + marker}, Env: []string{"PATH=/usr/bin:/bin"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, serr := os.Stat(marker); !errors.Is(serr, fs.ErrNotExist) {
		t.Fatalf("the process started: %v", serr)
	}
	// Nothing is even looked up: the cancellation is the error.
	missing := Cmd{Name: "herdr-dots-no-such-cli", Env: []string{"PATH=/usr/bin:/bin"}}
	if _, _, err := (ExecRunner{}).Run(ctx, missing); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled before any lookup or start", err)
	}
}
