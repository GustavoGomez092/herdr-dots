package toolloop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteToolsRefuseAgentConfigInAnySpelling(t *testing.T) {
	root, _ := draftRoot(t)
	for _, p := range []string{".claude/settings.json", ".CLAUDE/settings.local.json", "sub/.claude/commands/x.md", ".mcp.json", ".MCP.json", ".codex/config.toml"} {
		if _, err := runWrite(t, root, "write_file", map[string]any{"path": p, "content": "{}"}); !errors.Is(err, ErrDenied) {
			t.Errorf("write_file %s: %v, want ErrDenied", p, err)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			t.Errorf("%s was written", p)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte("{\"a\":1}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runWrite(t, root, "edit_file", map[string]any{"path": ".mcp.json", "old": "1", "new": "2"}); !errors.Is(err, ErrDenied) {
		t.Errorf("edit_file .mcp.json: %v, want ErrDenied", err)
	}
}

func TestDraftDenyAndAgentConfigMatchCaseInsensitively(t *testing.T) {
	for _, p := range []string{".claude/settings.json", "a/b/.Claude/x", ".mcp.json", ".codex/x", ".git/hooks/pre-commit"} {
		if !Denied(p, DraftDeny) {
			t.Errorf("%s not draft-denied", p)
		}
	}
	for _, p := range []string{"src/claude.go", "mcp.json", "docs/codex.md"} {
		if Denied(p, DraftDeny) {
			t.Errorf("%s draft-denied", p)
		}
	}
	for _, p := range []string{".github/workflows/ci.yml", ".GitHub/Workflows/x.yaml", ".gitlab-ci.yml", ".vscode/tasks.json", ".vscode/settings.json",
		".envrc", ".gitattributes", ".gitmodules", ".pre-commit-config.yaml", ".husky/pre-commit", "Makefile", "makefile", "web/package.json", ".npmrc"} {
		if !AgentConfig(p) {
			t.Errorf("%s not flagged as agent/CI config", p)
		}
	}
	for _, p := range []string{"src/app.go", ".github/CODEOWNERS", "package-lock.json.md", ".vscode/launch.json"} {
		if AgentConfig(p) {
			t.Errorf("%s flagged as agent/CI config", p)
		}
	}
}
