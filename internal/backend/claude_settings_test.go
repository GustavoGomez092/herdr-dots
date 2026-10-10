package backend

import (
	"encoding/json"
	"slices"
	"testing"
)

// Security review R1 (Phase 10 Task 11): `claude -p` skips the workspace
// trust dialog, so without a guard it loads the repository's
// .claude/settings.json and .claude/settings.local.json. Those files can name
// commands Claude Code runs (apiKeyHelper, awsAuthRefresh, awsCredentialExport,
// gcpAuthRefresh), set env (ANTHROPIC_BASE_URL sends the prompt and the
// developer's credential elsewhere) and enable plugins. A cloned repository is
// untrusted, so no claude-cli dot run may read its settings: drafts and
// research run --restricted (no user, project or local settings), every other
// run loads only the developer's own user settings (--setting-sources user).
func TestClaudeRunsNeverLoadRepositorySettings(t *testing.T) {
	c := ClaudeCLI{Environ: func() []string { return nil }}
	review := req()
	noRepo := req()
	noRepo.RepoRead = false
	memory := req()
	memory.Pass = PassMemory
	question := req()
	question.Question = "is this token check safe?"
	budgeted := req()
	budgeted.MaxBudgetUSD = 0.5
	budgeted.Args = []string{"--model"} // a dangling value-taking flag must not swallow the guard
	research := req()
	research.Pass, research.Web = PassResearch, true
	draft := req()
	draft.Pass, draft.Write = PassWrite, true
	for name, r := range map[string]Request{
		"review": review, "review without repo_read": noRepo, "memory pass": memory,
		"ask": question, "background with a budget cap": budgeted, "research": research, "draft": draft,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := c.BuildCmd(r)
			defer cmd.Close()
			restricted := slices.Contains(cmd.Args, "--restricted")
			src, hasSrc := argAfter(cmd.Args, "--setting-sources")
			switch {
			case restricted && hasSrc:
				t.Fatalf("--restricted already drops every settings file; --setting-sources must not be added: %q", cmd.Args)
			case restricted:
			case !hasSrc || src != "user":
				t.Fatalf("a run without --restricted must pass --setting-sources user, got %q (args %q)", src, cmd.Args)
			}
			if (r.Pass == PassResearch || WriteMode(r)) != restricted {
				t.Fatalf("only drafts and research run --restricted: %q", cmd.Args)
			}
			// The dot's args come last; a dangling value-taking flag there
			// would swallow a guard placed after it.
			if hasSrc && slices.Index(cmd.Args, "--setting-sources") >= len(cmd.Args)-len(r.Args) {
				t.Fatalf("the guard must come before the dot's own args: %q", cmd.Args)
			}
		})
	}
}

// A dot's own args can't turn the repository's settings back on: the flag
// is on the deny list (dropped from argv and refused by Run).
func TestDotArgsCannotReloadRepositorySettings(t *testing.T) {
	r := req()
	r.Args = []string{"--setting-sources", "user,project,local"}
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
	defer cmd.Close()
	n := 0
	for i, a := range cmd.Args {
		if a == "--setting-sources" {
			n++
			if cmd.Args[i+1] != "user" {
				t.Fatalf("setting sources %q", cmd.Args[i+1])
			}
		}
	}
	if n != 1 || slices.Contains(cmd.Args, "user,project,local") {
		t.Fatalf("args %q", cmd.Args)
	}
	if err := checkArgs("claude-cli", r.Args); err == nil {
		t.Fatal("Run must refuse a dot that passes --setting-sources")
	}
}

// The inline form of the flag is refused too (small round, item 3).
func TestDotArgsCannotReloadRepositorySettingsInline(t *testing.T) {
	r := req()
	r.Args = []string{"--setting-sources=user,project,local"}
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
	defer cmd.Close()
	if slices.Contains(cmd.Args, "--setting-sources=user,project,local") {
		t.Fatalf("the inline form reached argv: %q", cmd.Args)
	}
	if v, _ := argAfter(cmd.Args, "--setting-sources"); v != "user" {
		t.Fatalf("setting sources %q", v)
	}
	if err := checkArgs("claude-cli", r.Args); err == nil {
		t.Fatal("Run must refuse a dot that passes --setting-sources=…")
	}
}

// Security review R3 (cheap half): claude-cli review, ask and memory runs
// that read the repository get the secret-file Read deny rules research
// has (researchSettings: toolloop.DefaultDeny, DraftDeny, credentials*,
// tool_deny_globs), with hooks still off.
func TestClaudeRepoReadRunsDenySecretFiles(t *testing.T) {
	c := ClaudeCLI{Environ: func() []string { return nil }, Deny: []string{"private/**"}}
	review := req()
	memory := req()
	memory.Pass = PassMemory
	question := req()
	question.Question = "is this token check safe?"
	for name, r := range map[string]Request{"review": review, "memory pass": memory, "ask": question} {
		t.Run(name, func(t *testing.T) {
			cmd := c.BuildCmd(r)
			defer cmd.Close()
			v, _ := argAfter(cmd.Args, "--settings")
			var s struct {
				DisableAllHooks bool `json:"disableAllHooks"`
				Permissions     struct {
					Deny []string `json:"deny"`
				} `json:"permissions"`
			}
			if err := json.Unmarshal([]byte(v), &s); err != nil {
				t.Fatalf("settings %q: %v", v, err)
			}
			if !s.DisableAllHooks {
				t.Fatalf("hooks must stay off: %s", v)
			}
			for _, want := range []string{"Read(.env)", "Read(**/.env)", "Read(id_rsa*)", "Read(**/.ssh/**)", "Read(credentials*)", "Read(private/**)"} {
				if !slices.Contains(s.Permissions.Deny, want) {
					t.Errorf("deny rules miss %s: %s", want, v)
				}
			}
		})
	}
	noRepo := req()
	noRepo.RepoRead = false
	if v, _ := argAfter(c.BuildCmd(noRepo).Args, "--settings"); v != hooklessSettings {
		t.Fatalf("a run with no tools keeps the hookless settings, got %q", v)
	}
}
