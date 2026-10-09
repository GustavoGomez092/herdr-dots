//go:build live

package all

// Run with: go test -tags live ./internal/backend/all/ -run Live -v
// Each test makes one real call and skips when its key (or Ollama) is absent.
// Models come from env so no OpenAI or OpenRouter id is hard-coded:
// HERDR_DOTS_LIVE_OPENAI_MODEL, HERDR_DOTS_LIVE_OPENROUTER_MODEL,
// HERDR_DOTS_LIVE_OLLAMA_MODEL (Anthropic uses claude-haiku-4-5 to keep it cheap).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/transcript"
)

func liveReq(model string, repo string) backend.Request {
	return backend.Request{DotName: "Live", Purpose: "Point out one obvious bug if there is one.", Verbosity: "terse",
		Model: model, RepoRead: repo != "", Cwd: repo, Force: true,
		Turns: []transcript.Turn{{Index: 1, User: "add a divide helper", Assistant: "func Div(a, b int) int { return a / b }"}}}
}

func liveRun(t *testing.T, kind, envKey, model string, withRepo bool) {
	t.Helper()
	if envKey != "" && os.Getenv(envKey) == "" {
		t.Skipf("%s not set", envKey)
	}
	if model == "" && kind != backend.KindAnthropicAPI {
		t.Skip("no model configured for " + kind)
	}
	repo := ""
	if withRepo {
		repo = t.TempDir()
		_ = os.WriteFile(filepath.Join(repo, "div.go"), []byte("package m\nfunc Div(a, b int) int { return a / b }\n"), 0o600)
	}
	creds := credentials.File{Path: filepath.Join(t.TempDir(), "none.toml")}
	b, err := backend.NewWith(kind, backend.Options{Creds: creds, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	if err := b.Check(ctx); err != nil {
		t.Skipf("check: %v", err)
	}
	res, err := b.Run(ctx, liveReq(model, repo))
	if err != nil {
		t.Fatalf("live %s: %v", kind, err)
	}
	t.Logf("%s verdict %+v model %s tokens %d/%d cache %d/%d cost $%.5f known=%v tools=%d",
		kind, res.Verdict, res.Model, res.InputTokens, res.OutputTokens, res.CacheReadTokens, res.CacheWriteTokens, res.CostUSD, res.CostKnown, res.ToolCalls)
}

func TestLiveAnthropic(t *testing.T) {
	liveRun(t, backend.KindAnthropicAPI, "ANTHROPIC_API_KEY", "claude-haiku-4-5", true)
}

func TestLiveOpenAI(t *testing.T) {
	liveRun(t, backend.KindOpenAIAPI, "OPENAI_API_KEY", os.Getenv("HERDR_DOTS_LIVE_OPENAI_MODEL"), true)
}

func TestLiveOpenRouter(t *testing.T) {
	liveRun(t, backend.KindOpenRouter, "OPENROUTER_API_KEY", os.Getenv("HERDR_DOTS_LIVE_OPENROUTER_MODEL"), true)
}

func TestLiveOllama(t *testing.T) {
	liveRun(t, backend.KindOllama, "", os.Getenv("HERDR_DOTS_LIVE_OLLAMA_MODEL"), true)
}
