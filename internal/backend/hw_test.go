package backend

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// B6 (post-rc hardening): a CLI backend built without a runner (v1's
// New(kind, nil)) fails the run with an error instead of panicking.
func TestCLIBackendWithoutARunnerFailsInsteadOfPanicking(t *testing.T) {
	for _, kind := range []string{KindClaudeCLI, KindCodexCLI} {
		be, err := New(kind, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cc, ok := be.(CodexCLI); ok {
			cc.Environ = func() []string { return []string{"CODEX_HOME=" + t.TempDir()} }
			be = cc
		}
		_, err = be.Run(context.Background(), req())
		if !errors.Is(err, ErrNoRunner) {
			t.Fatalf("%s: err = %v, want ErrNoRunner", kind, err)
		}
	}
}

// B12 (post-rc hardening, test gap): FinishAPI sanitizes the verdict
// message of an API reply (terminal escapes and bidi controls dropped;
// memory op text is sanitized by memory.Apply).
func TestFinishAPISanitizesTheReply(t *testing.T) {
	text := `{"relevant":true,"severity":"warn","message":"Token \u001b[31mleak\u202e here","remember":[{"op":"add","scope":"dot","kind":"finding","text":"Keep \u001b]0;x\u0007tokens out"}]}`
	res, err := FinishAPI(req(), APIRun{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(res.Verdict.Message, "\x1b\u202e") || !strings.Contains(res.Verdict.Message, "leak") {
		t.Fatalf("message %q", res.Verdict.Message)
	}
}

// B12 (post-rc hardening): the unknown-kind error names the kinds there are.
func TestUnknownKindListsTheKinds(t *testing.T) {
	_, err := NewWith("claude-api", Options{})
	if err == nil || !strings.Contains(err.Error(), KindClaudeCLI) || !strings.Contains(err.Error(), KindCodexCLI) {
		t.Fatalf("err = %v, want the registered kinds listed", err)
	}
}
