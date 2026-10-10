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

// B3 (post-rc hardening): claude can exit 0 with is_error set; that error
// gets the same actionable hint as a failed exit.
func TestClaudeIsErrorOnExitZeroGetsTheHint(t *testing.T) {
	f := &fakeRunner{stdout: `{"is_error":true,"result":"Invalid API key · Please run /login","total_cost_usd":0}`}
	_, err := ClaudeCLI{Runner: f, Environ: func() []string { return nil }}.Run(context.Background(), req())
	if err == nil || !strings.Contains(err.Error(), "claude is not logged in") {
		t.Fatalf("err = %v, want the login hint", err)
	}
	f = &fakeRunner{stdout: `{"is_error":true,"result":"Overloaded","total_cost_usd":0.01}`}
	res, err := ClaudeCLI{Runner: f, Environ: func() []string { return nil }}.Run(context.Background(), req())
	if err == nil || !strings.Contains(err.Error(), "Overloaded") || !res.CostKnown || res.CostUSD != 0.01 {
		t.Fatalf("err = %v, res %+v: the error text and the spend stay", err, res)
	}
}

// B7 (post-rc hardening, test gap): a null "relevant" and non-string fields
// are malformed, never a verdict.
func TestVerdictWithNullOrWrongTypedFieldsIsMalformed(t *testing.T) {
	for _, text := range []string{
		`{"relevant":null,"message":"x"}`,
		`{"relevant":"yes","message":"x"}`,
		`{"relevant":true,"message":5}`,
		`{"relevant":true,"severity":["warn"],"message":"x"}`,
	} {
		if v, err := ParseVerdict(text); !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseVerdict(%s) = %+v, %v; want ErrMalformed", text, v, err)
		}
	}
}

// B12 (post-rc hardening): the unknown-kind error names the kinds there are.
func TestUnknownKindListsTheKinds(t *testing.T) {
	_, err := NewWith("claude-api", Options{})
	if err == nil || !strings.Contains(err.Error(), KindClaudeCLI) || !strings.Contains(err.Error(), KindCodexCLI) {
		t.Fatalf("err = %v, want the registered kinds listed", err)
	}
}
