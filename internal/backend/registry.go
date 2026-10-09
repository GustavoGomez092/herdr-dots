package backend

import "fmt"

// New returns the backend for a dot's [backend].kind.
func New(kind string, r Runner) (Backend, error) {
	switch kind {
	case "claude-cli":
		return ClaudeCLI{Runner: r}, nil
	case "codex-cli":
		return CodexCLI{Runner: r}, nil
	}
	return nil, fmt.Errorf("unknown backend %q", kind)
}
