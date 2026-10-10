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
	if v, _ := argAfter(cmd.Args, "--settings"); v != researchSettings(nil) {
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
	if v, _ := argAfter(cmd.Args, "--settings"); v != researchSettings(nil) || slices.Contains(cmd.Args, "{}") {
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

// P8-R15(a): a run the CLI reports as failed (is_error, e.g. stopped by
// --max-budget-usd) still spent money: its cost and usage stay in Result so
// the run is charged, while the error still surfaces.
func TestClaudeIsErrorKeepsCost(t *testing.T) {
	out := `{"type":"result","subtype":"error_max_budget_usd","is_error":true,"result":"budget exceeded","total_cost_usd":0.37,"usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":5,"cache_creation_input_tokens":7}}`
	res, err := ParseClaudeOutput([]byte(out))
	if err == nil {
		t.Fatal("is_error must still surface as an error")
	}
	if !res.CostKnown || res.CostUSD != 0.37 || res.InputTokens != 112 || res.OutputTokens != 20 {
		t.Fatalf("is_error with a cost must keep it: %+v", res)
	}
	if res.Verdict.Relevant || res.Verdict.Message != "" {
		t.Fatalf("a failed run has no verdict: %+v", res.Verdict)
	}
	for _, exit := range []error{nil, errors.New("exit status 1")} {
		run := &fakeRunner{stdout: out, err: exit}
		res, err := ClaudeCLI{Runner: run, Environ: func() []string { return nil }}.Run(context.Background(), req())
		if err == nil || !res.CostKnown || res.CostUSD != 0.37 || res.InputTokens != 112 {
			t.Fatalf("exit %v: Run must return the cost with the error: %+v %v", exit, res, err)
		}
	}
}

// P8-R15(b): a background run's dollar cap reaches claude as
// --max-budget-usd; a run without one (every foreground run) has no flag,
// and a non-positive cap is never passed.
func TestClaudeMaxBudgetFlag(t *testing.T) {
	c := ClaudeCLI{Environ: func() []string { return nil }}
	r := req()
	r.Pass, r.MaxBudgetUSD = PassResearch, 0.1
	if v, ok := argAfter(c.BuildCmd(r).Args, "--max-budget-usd"); !ok || v != "0.1" {
		t.Fatalf("--max-budget-usd = %q (%v), want 0.1", v, ok)
	}
	r.MaxBudgetUSD = 0.5
	if v, _ := argAfter(c.BuildCmd(r).Args, "--max-budget-usd"); v != "0.5" {
		t.Fatalf("--max-budget-usd = %q, want 0.5", v)
	}
	r.Args = []string{"--max-budget-usd", "99", "--max-budget-usd=77", "--model", "x"} // the dot's own caps are stripped
	args := c.BuildCmd(r).Args
	if countArg(args, "--max-budget-usd") != 1 || slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--max-budget-usd=") }) || slices.Contains(args, "99") || slices.Contains(args, "77") {
		t.Fatalf("the dot's --max-budget-usd must be removed: %v", args)
	}
	if v, _ := argAfter(args, "--max-budget-usd"); v != "0.5" {
		t.Fatalf("--max-budget-usd = %q, want 0.5", v)
	}
	// A dangling value flag at the end of the dot's args would take whatever
	// follows as its value, so the cap pair goes before the dot's args.
	r.Args = []string{"--model"}
	args = c.BuildCmd(r).Args
	i := slices.Index(args, "--max-budget-usd")
	if i < 0 || i+1 >= len(args) || args[i+1] != "0.5" || args[len(args)-1] != "--model" || i+2 > len(args)-1 {
		t.Fatalf("the cap pair must stay intact and precede the dot's args: %v", args)
	}
	r.Args = nil
	for _, v := range []float64{0, -0.2} {
		r.MaxBudgetUSD = v
		if slices.Contains(c.BuildCmd(r).Args, "--max-budget-usd") {
			t.Fatalf("cap %v must not be passed", v)
		}
	}
	if slices.Contains(c.BuildCmd(req()).Args, "--max-budget-usd") {
		t.Fatal("a review without a cap has no --max-budget-usd")
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

func countArg(args []string, a string) int {
	n := 0
	for _, x := range args {
		if x == a {
			n++
		}
	}
	return n
}
