//go:build unix

package platform

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestUnixConstants(t *testing.T) {
	if DevNull != "/dev/null" || EnvKey("Herdr_Pane_Id") != "Herdr_Pane_Id" || HomeEnv != "HOME" {
		t.Fatalf("DevNull %q, EnvKey must keep case on Unix", DevNull)
	}
}

// On Unix a child's environment is exactly what the caller built: no shell
// searches the current directory for a program name.
func TestChildEnvIsTheIdentityOnUnix(t *testing.T) {
	in := []string{"PATH=/bin", "A=b"}
	if got := ChildEnv(in); len(got) != 2 || got[0] != in[0] || got[1] != in[1] {
		t.Fatalf("ChildEnv = %q", got)
	}
	if got := ChildEnv(nil); got != nil {
		t.Fatalf("ChildEnv(nil) = %q, want nil (inherit)", got)
	}
}

func TestTreeKillEndsTheWholeProcessGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & sleep 30")
	var out bytes.Buffer
	cmd.Stdout = &out // the background sleep holds the pipe: Wait returns only when it dies too
	tree, err := StartTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if err := tree.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("tree took %v to die", d)
	}
}

func TestStartDetachedStartsTheProcess(t *testing.T) {
	p, err := StartDetached(exec.Command("sh", "-c", "exit 0"))
	if err != nil {
		t.Fatal(err)
	}
	if st, err := p.Wait(); err != nil || !st.Success() {
		t.Fatalf("detached child: %v %v", st, err)
	}
}

func TestCommandResolvesAtStartLikeV1(t *testing.T) {
	cmd, err := Command("definitely-not-a-program-xyz", []string{"a"})
	if err != nil || cmd.Start() == nil {
		t.Fatalf("Command must defer the not-found error to Start: %v", err)
	}
}

func TestCommandContextIsBoundToTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd, err := CommandContext(ctx, "sleep", []string{"5"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if cmd.Run() == nil || time.Since(start) > 2*time.Second {
		t.Fatal("a cancelled context must stop the command")
	}
}
