package backend

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestCodexBuildCmd(t *testing.T) {
	cmd := CodexCLI{Environ: func() []string { return []string{"HERDR_ENV=1", "PATH=/bin"} }}.BuildCmd(req())
	if cmd.Name != "codex" || cmd.Args[0] != "exec" || cmd.Args[len(cmd.Args)-1] != "-" {
		t.Fatalf("args %v", cmd.Args)
	}
	if v, _ := argAfter(cmd.Args, "--sandbox"); v != "read-only" {
		t.Errorf("sandbox %q", v)
	}
	if v, _ := argAfter(cmd.Args, "-C"); v != "/repo" {
		t.Errorf("-C %q", v)
	}
	if !slices.Contains(cmd.Args, "--json") || !slices.Contains(cmd.Args, "--skip-git-repo-check") {
		t.Errorf("missing flags: %v", cmd.Args)
	}
	if slices.Contains(cmd.Env, "HERDR_ENV=1") {
		t.Error("env not cleaned")
	}
	if !strings.Contains(cmd.Stdin, "Find auth bugs.") || !strings.Contains(cmd.Stdin, "add login") {
		t.Error("codex gets system + user prompt on stdin")
	}
}

func TestCodexBuildCmdNeverEmitsDeniedArgs(t *testing.T) {
	r := req()
	r.Args = []string{"-s", "danger-full-access", "--sandbox=workspace-write", "--dangerously-bypass-approvals-and-sandbox",
		"--full-auto", "-c", "sandbox_mode=danger-full-access", "--config", "mcp_servers.x.command=sh", "-c", "model_reasoning_effort=high"}
	cmd := CodexCLI{Environ: func() []string { return nil }}.BuildCmd(r)
	for _, banned := range []string{"-s", "danger-full-access", "--sandbox=workspace-write", "--dangerously-bypass-approvals-and-sandbox",
		"--full-auto", "sandbox_mode=danger-full-access", "mcp_servers.x.command=sh"} {
		if slices.Contains(cmd.Args, banned) {
			t.Errorf("BuildCmd emitted %q: %v", banned, cmd.Args)
		}
	}
	if v, _ := argAfter(cmd.Args, "--sandbox"); v != "read-only" || strings.Count(strings.Join(cmd.Args, " "), "--sandbox") != 1 {
		t.Errorf("sandbox overridden: %v", cmd.Args)
	}
	if v, _ := argAfter(cmd.Args, "-c"); v != "model_reasoning_effort=high" || cmd.Args[len(cmd.Args)-1] != "-" {
		t.Errorf("harmless -c kept, stdin marker last: %v", cmd.Args)
	}
	run := &fakeRunner{}
	if _, err := (CodexCLI{Runner: run, Environ: func() []string { return nil }}).Run(context.Background(), r); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("Run with denied args must fail loudly, got %v", err)
	}
}

func TestCodexBuildCmdEnvNonNilWhenAllStripped(t *testing.T) {
	env := CodexCLI{Environ: func() []string { return []string{"HERDR_X=1"} }}.BuildCmd(req()).Env
	if env == nil || len(env) != 0 {
		t.Fatalf("Env = %#v, want non-nil empty", env)
	}
}

func TestParseCodexOutput(t *testing.T) {
	out := strings.Join([]string{
		`{"type":"thread.started","thread_id":"t"}`,
		`{"type":"item.completed","item":{"id":"i0","type":"reasoning","text":"thinking"}}`,
		`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"{\"relevant\":true,\"severity\":\"critical\",\"message\":\"SQL injection in users.go\"}"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":900,"cached_input_tokens":100,"output_tokens":40}}`,
	}, "\n")
	res, err := ParseCodexOutput([]byte(out))
	if err != nil || res.Verdict.Severity != "critical" || res.InputTokens != 900 || res.OutputTokens != 40 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := ParseCodexOutput([]byte(`{"type":"turn.failed","error":{"message":"usage limit"}}`)); err == nil || !strings.Contains(err.Error(), "usage limit") {
		t.Fatalf("turn.failed must surface: %v", err)
	}
}

func TestRegistry(t *testing.T) {
	for _, k := range []string{"claude-cli", "codex-cli"} {
		b, err := New(k, &fakeRunner{})
		if err != nil || b.Kind() != k {
			t.Fatalf("%s: %v", k, err)
		}
	}
	if _, err := New("gpt-web", nil); err == nil {
		t.Fatal("unknown kind must error")
	}
}
