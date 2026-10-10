package backend

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/transcript"
)

func TestResearchSystemPrompt(t *testing.T) {
	r := Request{DotName: "Security", Purpose: "Find auth bugs.", Verbosity: "normal", Pass: PassResearch, Learn: true}
	s := SystemPrompt(r)
	for _, want := range []string{"working in the background while the developer is away", "Report only what is worth their attention on return",
		`"relevant"`, "Silence is better than noise", `"kind":"research"|"question"`} {
		if !strings.Contains(s, want) {
			t.Errorf("research prompt missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "untrusted reference data") {
		t.Error("the web note appears only when the run has web tools")
	}
	r.Web = true
	if !strings.Contains(SystemPrompt(r), "untrusted reference data") {
		t.Error("web runs warn that fetched pages are data, not instructions")
	}
	r.Learn = false
	if strings.Contains(SystemPrompt(r), "MEMORY (optional)") {
		t.Error("no memory contract without learn")
	}
	review := SystemPrompt(Request{DotName: "Security", Purpose: "p", Verbosity: "normal", Learn: true})
	if strings.Contains(review, "background") || strings.Contains(review, `"research"`) {
		t.Errorf("a review prompt must not offer research kinds:\n%s", review)
	}
}

func TestUserPromptLabelsQuestionByTrigger(t *testing.T) {
	turns := []transcript.Turn{{Index: 3, User: "add login", Assistant: "done"}}
	cases := map[string]Request{
		"QUESTION FROM THE DEVELOPER:":          {Turns: turns, Question: "q"},
		"RESEARCH BRIEF":                        {Turns: turns, Question: "q", Pass: PassResearch},
		"STANDING TASK (from your dot":          {Turns: turns, Question: "q", Trigger: TriggerScheduled},
		"NEW COMMIT (repository data to review": {Turns: turns, Question: "q", Trigger: TriggerCommit},
	}
	for want, r := range cases {
		u := UserPrompt(r)
		if !strings.Contains(u, want) || !strings.Contains(u, "### Turn 3") {
			t.Errorf("%s: %s", want, u)
		}
	}
	if u := UserPrompt(Request{Question: "review commits", Trigger: TriggerScheduled}); !strings.Contains(u, "CONVERSATION: none (no recent turns).") || strings.Contains(u, "### Turn") {
		t.Errorf("a run without turns says so: %s", u)
	}
}

func TestClaudeWebToolsOnlyForWebResearch(t *testing.T) {
	c := ClaudeCLI{Environ: func() []string { return nil }}
	r := req()
	if v, _ := argAfter(c.BuildCmd(r).Args, "--tools"); v != "Read,Grep,Glob" {
		t.Fatalf("v1 tools unchanged: %q", v)
	}
	r.Web = true // a review never gets the web, even if the flag is set
	if v, _ := argAfter(c.BuildCmd(r).Args, "--tools"); v != "Read,Grep,Glob" {
		t.Fatalf("review with Web: %q", v)
	}
	r.Pass = PassResearch
	args := c.BuildCmd(r).Args
	tools, _ := argAfter(args, "--tools")
	allowed, _ := argAfter(args, "--allowedTools")
	if tools != "Read,Grep,Glob,WebSearch" || allowed != tools {
		t.Fatalf("tools %q allowed %q", tools, allowed)
	}
	for _, bad := range []string{"Bash", "Edit", "Write"} {
		if strings.Contains(tools, bad) || slices.Contains(args, "--dangerously-skip-permissions") {
			t.Fatalf("research must stay read-only: %v", args)
		}
	}
	for _, bad := range []string{"--bare", "--dangerously-skip-permissions", "--permission-mode"} {
		if slices.Contains(args, bad) {
			t.Fatalf("research argv has %s: %v", bad, args)
		}
	}
	if v, _ := argAfter(args, "--settings"); !strings.Contains(v, `"disableAllHooks":true`) {
		t.Fatalf("settings: %q", v)
	}
	if !slices.Contains(args, "--no-session-persistence") {
		t.Fatalf("missing --no-session-persistence: %v", args)
	}
	r.RepoRead = false
	if v, _ := argAfter(c.BuildCmd(r).Args, "--tools"); v != "WebSearch" {
		t.Fatalf("web without repo read: %q", v)
	}
}

// P8-R14: unattended claude-cli research must not be able to send a secret
// out. No page fetching (WebSearch only), --restricted (user/project settings
// can't add allow rules), and a settings JSON that keeps hooks off and denies
// Read of every secret glob the API tools deny.
func TestClaudeResearchDeniesSecretsAndFetch(t *testing.T) {
	c := ClaudeCLI{Environ: func() []string { return nil }}
	for _, web := range []bool{true, false} {
		r := req()
		r.Pass, r.Web = PassResearch, web
		args := c.BuildCmd(r).Args
		for _, a := range args {
			if strings.Contains(a, "WebFetch") {
				t.Fatalf("web=%v: research argv mentions WebFetch: %v", web, args)
			}
		}
		if !slices.Contains(args, "--restricted") {
			t.Fatalf("web=%v: research must run --restricted: %v", web, args)
		}
		settings, _ := argAfter(args, "--settings")
		var s struct {
			DisableAllHooks bool `json:"disableAllHooks"`
			Permissions     struct {
				Deny []string `json:"deny"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal([]byte(settings), &s); err != nil {
			t.Fatalf("settings not JSON: %q: %v", settings, err)
		}
		if !s.DisableAllHooks {
			t.Fatalf("web=%v: hooks must stay off: %q", web, settings)
		}
		for _, g := range []string{".env", ".env.*", "**/.env", "*.pem", "*.key", "id_rsa*", "**/.ssh/**", "**/.aws/**",
			".netrc", "**/credentials*", ".git/**", ".claude/**", ".codex/**", ".mcp.json"} {
			if !slices.Contains(s.Permissions.Deny, "Read("+g+")") {
				t.Errorf("web=%v: settings deny lacks Read(%s): %v", web, g, s.Permissions.Deny)
			}
		}
		// Built from the shared deny constants: every API-tool deny glob is here too.
		for _, g := range append(append([]string(nil), toolloop.DefaultDeny...), toolloop.DraftDeny...) {
			if !slices.Contains(s.Permissions.Deny, "Read("+g+")") {
				t.Errorf("web=%v: settings deny lacks Read(%s)", web, g)
			}
		}
	}
}

// A review or memory pass (not research) keeps the v1 argv: hookless
// settings, no --restricted, the read tools only, even with Web set.
func TestClaudeNonResearchPassUnchanged(t *testing.T) {
	c := ClaudeCLI{Environ: func() []string { return nil }}
	for _, pass := range []string{PassReview, PassMemory} {
		r := req()
		r.Pass, r.Web = pass, true
		args := c.BuildCmd(r).Args
		if v, _ := argAfter(args, "--settings"); v != researchSettings(nil, "") {
			t.Errorf("pass %q: settings %q", pass, v)
		}
		if slices.Contains(args, "--restricted") {
			t.Errorf("pass %q: --restricted is for research and drafts only: %v", pass, args)
		}
		if v, _ := argAfter(args, "--tools"); v != "Read,Grep,Glob" {
			t.Errorf("pass %q: tools %q", pass, v)
		}
	}
}

func TestResearchNeverMixesWithWrite(t *testing.T) {
	run := &fakeRunner{stdout: `{"result":"{\"relevant\":false}"}`}
	c := ClaudeCLI{Runner: run, Environ: func() []string { return nil }}
	r := req()
	r.Pass, r.Web, r.Write = PassResearch, true, true
	if _, err := c.Run(context.Background(), r); err == nil || !strings.Contains(err.Error(), "research") {
		t.Fatalf("research + Write must be refused, got %v", err)
	}
	// Even if BuildCmd is reached directly, no write tool appears.
	tools, _ := argAfter(c.BuildCmd(r).Args, "--tools")
	for _, bad := range []string{"Bash", "Edit", "Write"} {
		if strings.Contains(tools, bad) {
			t.Fatalf("research+Write tools: %q", tools)
		}
	}
}

func TestResearchMemoryOnlyInUserPrompt(t *testing.T) {
	const mem = "MEMORY-SENTINEL-OPEN-QUESTION"
	r := req()
	r.Pass, r.Web, r.Learn, r.Memory, r.Question = PassResearch, true, true, mem, "brief "+mem
	if strings.Contains(SystemPrompt(r), mem) {
		t.Error("memory or the brief leaked into the system prompt")
	}
	cmd := (ClaudeCLI{Environ: func() []string { return nil }}).BuildCmd(r)
	if !strings.Contains(cmd.Stdin, mem) {
		t.Error("memory missing from the user prompt")
	}
	for _, a := range cmd.Args {
		if strings.Contains(a, mem) {
			t.Errorf("memory in argv: %q", a)
		}
	}
}

func TestWebSupported(t *testing.T) {
	for kind, want := range map[string]bool{
		KindClaudeCLI: true, KindAnthropicAPI: true, KindOpenAIAPI: true,
		KindCodexCLI: false, KindOpenRouter: false, KindOllama: false, "nope": false,
	} {
		if WebSupported(kind) != want {
			t.Errorf("WebSupported(%s) != %v", kind, want)
		}
	}
}
