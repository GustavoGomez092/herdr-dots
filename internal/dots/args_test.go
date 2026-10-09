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
