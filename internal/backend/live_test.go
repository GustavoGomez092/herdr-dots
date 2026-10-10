//go:build live

package backend

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// Run with: go test -tags live ./internal/backend/ -run Live -v
func TestLiveClaude(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	r := req()
	r.Model = "haiku"
	r.RepoRead = false
	r.Cwd = t.TempDir()
	res, err := New("claude-cli", ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := res.Run(ctx, r)
	if err != nil {
		t.Fatalf("live claude: %v", err)
	}
	t.Logf("verdict %+v tokens %d/%d", out.Verdict, out.InputTokens, out.OutputTokens)
}

func TestLiveCodex(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	r := req()
	r.Model = ""
	r.Cwd = t.TempDir()
	b, err := New("codex-cli", ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.Run(ctx, r)
	if err != nil {
		t.Fatalf("live codex: %v", err)
	}
	t.Logf("verdict %+v", out.Verdict)
}
