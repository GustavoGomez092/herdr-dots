package dots

import (
	"slices"
	"strings"
	"testing"
)

var claudeDenied = [][]string{
	{"--dangerously-skip-permissions"},
	{"--allow-dangerously-skip-permissions"},
	{"--permission-mode", "bypassPermissions"},
	{"--permission-mode=bypassPermissions"},
	{"--tools", "Bash,Edit"},
	{"--tools=Bash"},
	{"--allowedTools", "Bash"},
	{"--allowed-tools", "Bash"},
	{"--disallowedTools", "Read"},
	{"--disallowed-tools", "Read"},
	{"--settings", `{"disableAllHooks":false}`},
	{"--mcp-config", "x.json"},
	{"--append-system-prompt-file", "/etc/passwd"},
	{"--append-system-prompt-file=/etc/passwd"},
	{"--add-dir", "/"},
	{"--bare"},
	{"--restricted"},
	{"--setting-sources", "x"},
	{"--setting-sources=x"},
	{"-w", "x"},
	{"--worktree", "x"},
	{"--worktree=x"},
	{"--plugin-dir", "x"},
	{"--plugin-dir=x"},
	{"--plugin-url", "x"},
	{"--plugin-url=x"},
	{"--agent", "x"},
	{"--agent=x"},
	{"--agents", "x"},
	{"--agents=x"},
	{"--resume", "x"},
	{"--resume=x"},
	{"--continue"},
	{"--system-prompt", "x"},
	{"--system-prompt=x"},
	{"-wx"},
}

var codexDenied = [][]string{
	{"--sandbox", "danger-full-access"},
	{"--sandbox=danger-full-access"},
	{"-s", "danger-full-access"},
	{"-sdanger-full-access"},
	{"--dangerously-bypass-approvals-and-sandbox"},
	{"--yolo"},
	{"--full-auto"},
	{"-c", "sandbox_mode=danger-full-access"},
	{"-csandbox_mode=danger-full-access"},
	{"--config", "mcp_servers.x.command=sh"},
	{"--config=sandbox_workspace_write.network_access=true"},
	{"--add-dir", "/"},
	{"-C", "/"},
	{"-C/"},
	{"--cd", "/"},
	{"--cd=/"},
}

func TestValidateRejectsUnsafeBackendArgs(t *testing.T) {
	for kind, cases := range map[string][][]string{"claude-cli": claudeDenied, "codex-cli": codexDenied} {
		for _, args := range cases {
			d := New("Security")
			d.Purpose = "p"
			d.Backend.Kind = kind
			d.Backend.Args = append([]string{"--verbose"}, args...)
			err := d.Validate()
			if err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Errorf("%s %v: Validate = %v, want a not-allowed error", kind, args, err)
			}
		}
	}
}

func TestValidateAllowsHarmlessBackendArgs(t *testing.T) {
	for kind, args := range map[string][]string{
		"claude-cli": {"--verbose", "--fallback-model", "haiku"},
		"codex-cli":  {"-c", "model_reasoning_effort=high", "--config=hide_agent_reasoning=true"},
	} {
		d := New("Security")
		d.Purpose = "p"
		d.Backend.Kind = kind
		d.Backend.Args = args
		if err := d.Validate(); err != nil {
			t.Errorf("%s %v: %v", kind, args, err)
		}
	}
}

func TestSafeArgsDropsDeniedFlagsAndTheirValues(t *testing.T) {
	got := SafeArgs("claude-cli", []string{"--verbose", "--allowedTools", "Bash", "Edit", "--dangerously-skip-permissions", "--fallback-model", "haiku", "--permission-mode=bypassPermissions"})
	if want := []string{"--verbose", "--fallback-model", "haiku"}; !slices.Equal(got, want) {
		t.Fatalf("claude SafeArgs = %v, want %v", got, want)
	}
	got = SafeArgs("codex-cli", []string{"-s", "danger-full-access", "-c", "model=x", "-c", "sandbox_mode=danger-full-access", "--full-auto"})
	if want := []string{"-c", "model=x"}; !slices.Equal(got, want) {
		t.Fatalf("codex SafeArgs = %v, want %v", got, want)
	}
}

// P9-R4: args that route prompts elsewhere, enable web or load another
// system prompt are refused for every dot, local ones included, and the
// error names the arg.
func TestValidateRefusesArgsThatRoutePromptsOrEnableWeb(t *testing.T) {
	for kind, cases := range map[string][][]string{
		"claude-cli": {
			{"--system-prompt-file", "/tmp/evil.md"},
			{"--system-prompt-file=/tmp/evil.md"},
		},
		"codex-cli": {
			{"-c", "model_provider=evil"},
			{"-c", "model_providers.evil.base_url=https://evil.invalid/v1"},
			{"--config", "openai_base_url=https://evil.invalid"},
			{"--config=tools.web_search=true"},
			{"-c", "web_search=live"},
			{"-cshell_environment_policy.inherit=all"},
			{"-c", "approval_policy=never"},
			{"-c", "profile=evil"},
			{"-c", "env.X=1"},
			{"-c", "notify=[\"sh\"]"}, // not on the allowlist
			{"-c", "'model'=x"},
			{"-p", "evil"},
			{"--profile=evil"},
			{"--oss"},
			{"--local-provider", "ollama"},
			{"--enable", "web_search_request"},
			{"--search"},
			{"-o", "/tmp/out"},
			{"--output-last-message=/tmp/out"},
		},
	} {
		for _, args := range cases {
			d := New("Security")
			d.Purpose = "p"
			d.Backend.Kind = kind
			d.Backend.Args = args
			err := d.Validate()
			if err == nil || !strings.Contains(err.Error(), "not allowed") || !strings.Contains(err.Error(), args[len(args)-1]) {
				t.Errorf("%s %v: Validate = %v, want a not-allowed error naming the arg", kind, args, err)
			}
		}
	}
	d := New("Codex")
	d.Purpose = "p"
	d.Backend.Kind = "codex-cli"
	d.Backend.Args = []string{"-c", "model=o4", "-c", "model_reasoning_effort=high", "-c", "model_reasoning_summary=auto", "--config=hide_agent_reasoning=true"}
	if err := d.Validate(); err != nil {
		t.Fatalf("allow-listed codex config keys: %v", err)
	}
}

// P9-R4: someone else's dot keeps only --model/-m and --effort with plain
// values; every other arg is removed, and that can't be kept.
func TestClampKeepsOnlyModelAndEffortArgs(t *testing.T) {
	codex := riskyDot()
	codex.Backend.Kind = "codex-cli"
	codex.Capabilities = readOnly(codex.Capabilities)
	codex.Schedule.Task = "t"
	codex.Backend.Args = []string{"-c", "model_providers.evil.base_url=https://evil.invalid/v1", "--config=tools.web_search=true", "-m", "o4-mini", "--verbose"}
	got, cs := ClampDot(codex, Capabilities)
	if !slices.Equal(got.Backend.Args, []string{"-m", "o4-mini"}) {
		t.Fatalf("codex args %v", got.Backend.Args)
	}
	var argClamp *Clamp
	for i := range cs {
		if cs[i].Capability == CapArgs {
			argClamp = &cs[i]
		}
	}
	if argClamp == nil || argClamp.Keepable || !strings.Contains(argClamp.From, "tools.web_search=true") || !strings.Contains(argClamp.From, "--verbose") {
		t.Fatalf("args clamp %+v", argClamp)
	}
	if err := codex.Validate(); err == nil {
		t.Fatal("the same args are refused by Validate")
	}
	got.Backend.BaseURL = "" // codex-cli has none
	if err := got.Validate(); err != nil {
		t.Fatalf("clamped codex dot: %v", err)
	}

	claude := riskyDot()
	claude.Backend.Args = []string{"--model", "x", "--effort=high", "--model=claude-opus-5-5", "--fallback-model", "haiku",
		"--effort", "turbo", "--model", "$(curl evil)", "--model", "-x", "--system-prompt-file", "/tmp/p", "--model"}
	got, _ = ClampDot(claude, nil)
	if want := []string{"--model", "x", "--effort=high", "--model=claude-opus-5-5"}; !slices.Equal(got.Backend.Args, want) {
		t.Fatalf("claude args %v, want %v", got.Backend.Args, want)
	}
	plain := New("Plain")
	plain.Backend.Args = []string{"--model", "x"}
	if got, cs := ClampDot(plain, nil); !slices.Equal(got.Backend.Args, []string{"--model", "x"}) || slices.ContainsFunc(cs, func(c Clamp) bool { return c.Capability == CapArgs }) {
		t.Fatalf("--model x survives the clamp untouched: %v %+v", got.Backend.Args, cs)
	}
}
