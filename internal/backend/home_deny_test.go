package backend

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/credentials"
)

// Security review R3 (post-rc hardening): a claude-cli run that has the read
// tools can name any path, not only the repository's. Every such run's
// settings deny Read on the developer's home secrets (Claude Code's `~/`
// rules) and on the herdr-dots config directory that holds credentials.toml
// (an absolute `//` rule; the directory comes from the credentials path the
// engine passes in, never a hard-coded one).
func TestClaudeRepoReadRunsDenyHomeSecretsAndTheConfigDir(t *testing.T) {
	dir := t.TempDir()
	be, err := NewWith(KindClaudeCLI, Options{Creds: credentials.File{Path: filepath.Join(dir, "credentials.toml")}})
	if err != nil {
		t.Fatal(err)
	}
	c := be.(ClaudeCLI)
	c.Environ = func() []string { return nil }

	want := []string{
		"Read(~/.ssh/**)", "Read(~/.aws/**)", "Read(~/.gnupg/**)", "Read(~/.config/gh/**)",
		"Read(~/.netrc)", "Read(~/.docker/config.json)", "Read(~/.kube/**)",
		"Read(~/.claude.json)", "Read(~/.claude/**)", "Read(~/.codex/**)",
		"Read(/" + filepath.ToSlash(dir) + "/**)",
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		// macOS: /var is /private/var; the resolved form is denied too.
		want = append(want, "Read(/"+filepath.ToSlash(real)+"/**)")
	}

	review := req()
	memory := req()
	memory.Pass = PassMemory
	question := req()
	question.Question = "is this token check safe?"
	research := req()
	research.Pass, research.Web = PassResearch, true
	draft := writeReq() // a draft keeps its Edit/Write rules and gets the Read denies too
	for name, r := range map[string]Request{"review": review, "memory pass": memory, "ask": question, "research": research, "draft": draft} {
		t.Run(name, func(t *testing.T) {
			cmd := c.BuildCmd(r)
			defer cmd.Close()
			v, _ := argAfter(cmd.Args, "--settings")
			var s struct {
				Permissions struct {
					Deny []string `json:"deny"`
				} `json:"permissions"`
			}
			if err := json.Unmarshal([]byte(v), &s); err != nil {
				t.Fatalf("settings %q: %v", v, err)
			}
			for _, w := range want {
				if !slices.Contains(s.Permissions.Deny, w) {
					t.Errorf("deny rules miss %s: %s", w, v)
				}
			}
		})
	}
}

// Without a credentials path (tests, v1's New) the home rules still apply and
// no config-directory rule is invented.
func TestClaudeHomeDenyNeedsNoConfigDir(t *testing.T) {
	var s struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(researchSettings(nil, "")), &s); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.Permissions.Deny, "Read(~/.ssh/**)") {
		t.Fatalf("home rules missing: %v", s.Permissions.Deny)
	}
	for _, r := range s.Permissions.Deny {
		if strings.HasPrefix(r, "Read(//") {
			t.Fatalf("an absolute rule appeared without a config dir: %s", r)
		}
	}
}
