package toolloop

// DraftDeny lists paths no draft may create or change, whatever the backend:
// agent and tool configuration that would run code or widen permissions the
// next time an agent starts in the repository. worktree.Validate refuses a
// patch touching them (with DefaultDeny and tool_deny_globs) and the API
// write tools refuse to write them. Matched with Denied (case folded, at any
// depth).
var DraftDeny = []string{".claude/**", "**/.claude/**", ".git/**", ".mcp.json", ".codex/**"}

// FlagAgentConfig is the patch flag for a change to agent or CI
// configuration: allowed, but loudly flagged on the card and in the viewer.
const FlagAgentConfig = "agent/CI config"

// AgentConfigGlobs are files that make CI, editors, git or package scripts
// run code: a draft may change them, but the user is warned.
var AgentConfigGlobs = []string{
	".github/workflows/**", ".gitlab-ci.yml", ".vscode/tasks.json", ".vscode/settings.json", ".envrc",
	".gitattributes", ".gitmodules", ".pre-commit-config.yaml", ".husky/**", "Makefile", "package.json", ".npmrc",
	// Agent instruction files: a change steers the next agent that starts here.
	"CLAUDE.md", "AGENTS.md", ".cursor/**", ".cursorrules", ".github/copilot-instructions.md", ".windsurfrules",
}

// AgentConfig reports whether rel (slash-separated, repository-relative) is
// agent or CI configuration (AgentConfigGlobs, case folded, at any depth).
func AgentConfig(rel string) bool { return Denied(rel, AgentConfigGlobs) }

// AgentConfigAny reports whether any of paths is agent or CI configuration.
func AgentConfigAny(paths []string) bool {
	for _, p := range paths {
		if AgentConfig(p) {
			return true
		}
	}
	return false
}
