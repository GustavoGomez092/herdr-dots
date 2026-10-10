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

// B12 (post-rc hardening): the unknown-kind error names the kinds there are.
func TestUnknownKindListsTheKinds(t *testing.T) {
	_, err := NewWith("claude-api", Options{})
	if err == nil || !strings.Contains(err.Error(), KindClaudeCLI) || !strings.Contains(err.Error(), KindCodexCLI) {
		t.Fatalf("err = %v, want the registered kinds listed", err)
	}
}
