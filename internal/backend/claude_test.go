package backend

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/transcript"
)

type fakeRunner struct {
	got    Cmd
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, c Cmd) ([]byte, []byte, error) {
	f.got = c
	return []byte(f.stdout), []byte(f.stderr), f.err
}

func req() Request {
	return Request{DotName: "Security", Purpose: "Find auth bugs.", Verbosity: "normal", Model: "sonnet", RepoRead: true, Cwd: "/repo",
		Turns: []transcript.Turn{{Index: 1, User: "add login", Assistant: "done"}}}
}

func argAfter(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

func TestClaudeBuildCmdIsReadOnlyAndHookless(t *testing.T) {
	c := ClaudeCLI{Environ: func() []string { return []string{"PATH=/bin", "HERDR_PANE_ID=x"} }}
	cmd := c.BuildCmd(req())
	if cmd.Name != "claude" || cmd.Dir != "/repo" {
		t.Fatalf("cmd %+v", cmd)
	}
	for _, f := range []string{"-p", "--no-session-persistence", "--strict-mcp-config"} {
		if !slices.Contains(cmd.Args, f) {
			t.Errorf("missing %s", f)
		}
	}
	if v, _ := argAfter(cmd.Args, "--output-format"); v != "json" {
		t.Errorf("output-format %q", v)
	}
	if v, _ := argAfter(cmd.Args, "--settings"); v != `{"disableAllHooks":true}` {
		t.Errorf("settings %q", v)
	}
	if v, _ := argAfter(cmd.Args, "--tools"); v != "Read,Grep,Glob" {
		t.Errorf("tools %q", v)
	}
	if v, _ := argAfter(cmd.Args, "--model"); v != "sonnet" {
		t.Errorf("model %q", v)
	}
	if v, _ := argAfter(cmd.Args, "--append-system-prompt"); !strings.Contains(v, "Find auth bugs.") {
		t.Errorf("system prompt not passed")
	}
	for _, banned := range []string{"--bare", "--dangerously-skip-permissions", "bypassPermissions"} {
		if slices.Contains(cmd.Args, banned) {
			t.Errorf("must never pass %s", banned)
		}
	}
	if slices.Contains(cmd.Env, "HERDR_PANE_ID=x") {
		t.Error("HERDR_ vars must be stripped")
	}
	if !strings.Contains(cmd.Stdin, "add login") {
		t.Error("turns go on stdin")
	}
	noRepo := req()
	noRepo.RepoRead = false
	if v, ok := argAfter(c.BuildCmd(noRepo).Args, "--tools"); !ok || v != "" {
		t.Errorf("without repo_read tools must be empty, got %q", v)
	}
}

func TestParseClaudeOutput(t *testing.T) {
	out := `{"type":"result","is_error":false,"result":"{\"relevant\":true,\"severity\":\"warn\",\"message\":\"Use httpOnly\"}","usage":{"input_tokens":120,"output_tokens":30},"total_cost_usd":0.01}`
	res, err := ParseClaudeOutput([]byte(out))
	if err != nil || !res.Verdict.Relevant || res.Verdict.Message != "Use httpOnly" || res.InputTokens != 120 || res.OutputTokens != 30 || res.CostUSD != 0.01 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := ParseClaudeOutput([]byte(`{"type":"result","is_error":true,"result":"API overloaded"}`)); err == nil || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("is_error must surface: %v", err)
	}
	if _, err := ParseClaudeOutput([]byte(`not json`)); err == nil {
		t.Fatal("non-JSON must error")
	}
}

func TestClaudeRunMapsLoginFailureToHint(t *testing.T) {
	r := &fakeRunner{stdout: "", stderr: "Invalid API key · Please run /login", err: errors.New("exit status 1")}
	_, err := ClaudeCLI{Runner: r, Environ: func() []string { return nil }}.Run(context.Background(), req())
	if err == nil || !strings.Contains(err.Error(), "/login") {
		t.Fatalf("want login hint, got %v", err)
	}
}

func TestClaudeCheckMissingBinary(t *testing.T) {
	err := ClaudeCLI{Bin: "definitely-not-a-real-binary-xyz"}.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Check = %v", err)
	}
}
