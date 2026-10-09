package backend

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/platform"
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

func TestCleanEnvFoldsCaseWhereTheOSDoes(t *testing.T) {
	in := []string{"Herdr_Pane_Id=w1:p1", "claudecode=1", "Claude_Code_Entrypoint=cli", "Path=/bin"}
	got := CleanEnv(in)
	if platform.EnvKey("a") == "A" { // Windows: one variable whatever its case
		if !slices.Equal(got, []string{"Path=/bin"}) {
			t.Fatalf("Windows CleanEnv = %v", got)
		}
		return
	}
	if !slices.Equal(got, in) { // Unix: different names, kept as in v1
		t.Fatalf("Unix CleanEnv = %v", got)
	}
}

func TestCmdCloseRunsCleanupOnce(t *testing.T) {
	Cmd{}.Close() // nil Cleanup is fine
	n := 0
	Cmd{Cleanup: func() { n++ }}.Close()
	if n != 1 {
		t.Fatalf("cleanup ran %d times", n)
	}
}

func TestPreflightRecognisesShimErrors(t *testing.T) {
	if !Preflight(fmt.Errorf("x: %w", platform.ErrCmdTooLong)) || !Preflight(platform.ErrUnsafeBatchArg) || Preflight(errors.New("exit 1")) {
		t.Fatal("Preflight")
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
