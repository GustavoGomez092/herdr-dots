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

// Review: a dot's [backend].args must never override the read-only flags.
func TestClaudeBuildCmdNeverEmitsDeniedArgs(t *testing.T) {
	r := req()
	r.Args = []string{"--verbose", "--dangerously-skip-permissions", "--permission-mode", "bypassPermissions",
		"--tools", "Bash", "--allowedTools", "Bash", "Edit", "--settings", `{}`, "--mcp-config", "m.json", "--add-dir", "/", "--bare"}
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
	for _, banned := range []string{"--dangerously-skip-permissions", "--permission-mode", "bypassPermissions", "Bash", "Edit", "--mcp-config", "m.json", "--add-dir", "--bare"} {
		if slices.Contains(cmd.Args, banned) {
			t.Errorf("BuildCmd emitted %q: %v", banned, cmd.Args)
		}
	}
	for _, ours := range []string{"--tools", "--allowedTools"} {
		if n := strings.Count(strings.Join(cmd.Args, "\x00"), "\x00"+ours+"\x00"); n != 1 {
			t.Errorf("%s must appear exactly once (ours), got %d: %v", ours, n, cmd.Args)
		}
	}
	if v, _ := argAfter(cmd.Args, "--settings"); v != `{"disableAllHooks":true}` || slices.Contains(cmd.Args, "{}") {
		t.Errorf("settings overridden: %v", cmd.Args)
	}
	if !slices.Contains(cmd.Args, "--verbose") {
		t.Error("harmless args are kept")
	}
	run := &fakeRunner{stdout: `{"result":"{\"relevant\":false}"}`}
	if _, err := (ClaudeCLI{Runner: run, Environ: func() []string { return nil }}).Run(context.Background(), r); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("Run with denied args must fail loudly, got %v", err)
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

func TestClaudeBuildCmdEnvNonNilWhenAllStripped(t *testing.T) {
	env := ClaudeCLI{Environ: func() []string { return []string{"HERDR_X=1"} }}.BuildCmd(req()).Env
	if env == nil || len(env) != 0 {
		t.Fatalf("Env = %#v, want non-nil empty", env)
	}
}

func TestClaudeCheckMissingBinary(t *testing.T) {
	err := ClaudeCLI{Bin: "definitely-not-a-real-binary-xyz"}.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Check = %v", err)
	}
}
