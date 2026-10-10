package backend

import (
	"encoding/json"
	"slices"
	"testing"
)

// Ruling P10-R7 (S18): the config's tool_deny_globs reach a claude-cli
// research run's settings too, wired through the registry (Options.Deny),
// denied at the root and at any depth like the built-in globs.
func TestClaudeResearchDeniesTheConfiguredGlobs(t *testing.T) {
	be, err := NewWith(KindClaudeCLI, Options{Deny: []string{"secrets/**", "*.vault"}})
	if err != nil {
		t.Fatal(err)
	}
	c := be.(ClaudeCLI)
	c.Environ = func() []string { return nil }
	r := req()
	r.Pass = PassResearch
	settings, _ := argAfter(c.BuildCmd(r).Args, "--settings")
	var s struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(settings), &s); err != nil {
		t.Fatalf("settings not JSON: %q: %v", settings, err)
	}
	for _, g := range []string{"secrets/**", "**/secrets/**", "*.vault", "**/*.vault", ".env", "**/.ssh/**"} {
		if !slices.Contains(s.Permissions.Deny, "Read("+g+")") {
			t.Errorf("settings deny lacks Read(%s): %v", g, s.Permissions.Deny)
		}
	}
}
