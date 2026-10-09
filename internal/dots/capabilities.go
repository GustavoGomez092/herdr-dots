package dots

import (
	"fmt"
	"slices"
	"time"
)

// CapabilitySpec is a dot's [capabilities] table. Dot files without one get
// DefaultCapabilities: the dot can't write. Writing also needs the global
// write_enabled switch in config.toml, and every draft needs two approvals.
type CapabilitySpec struct {
	Write               bool `toml:"write"`                 // may draft fixes in an isolated worktree
	WriteTimeoutSeconds int  `toml:"write_timeout_seconds"` // one draft run
	MaxPatchBytes       int  `toml:"max_patch_bytes"`       // larger patches are rejected
	MaxFiles            int  `toml:"max_files"`             // patches touching more files are rejected
	MCP                 bool `toml:"mcp"`                   // Phase 9: the MCP server may ask this dot (ask_dot, ask_council)
}

func DefaultCapabilities() CapabilitySpec {
	return CapabilitySpec{WriteTimeoutSeconds: 600, MaxPatchBytes: 200000, MaxFiles: 50, MCP: true}
}

// WriteTimeout is the time limit of one draft run.
func (c CapabilitySpec) WriteTimeout() time.Duration {
	return time.Duration(c.WriteTimeoutSeconds) * time.Second
}

// WriteKinds are the backends that can draft without a shell (decision 5:
// write-capable dots get none). codex-cli is missing on purpose: `codex exec`
// always offers its shell tool, and no flag to turn it off is verified.
var WriteKinds = []string{"claude-cli", "anthropic-api", "openai-api", "openrouter", "ollama"}

// CanWrite reports whether a dot of this backend kind may set write = true.
func CanWrite(kind string) bool { return slices.Contains(WriteKinds, kind) }

func (c CapabilitySpec) validate(kind string) []error {
	var errs []error
	if c.Write && !CanWrite(kind) {
		errs = append(errs, fmt.Errorf("capabilities write needs one of %v: %s always has a shell, and write-capable dots get none", WriteKinds, kind))
	}
	if c.WriteTimeoutSeconds < 60 || c.WriteTimeoutSeconds > 3600 {
		errs = append(errs, fmt.Errorf("capabilities write_timeout_seconds must be 60-3600, got %d", c.WriteTimeoutSeconds))
	}
	if c.MaxPatchBytes < 1000 || c.MaxPatchBytes > 2000000 {
		errs = append(errs, fmt.Errorf("capabilities max_patch_bytes must be 1000-2000000, got %d", c.MaxPatchBytes))
	}
	if c.MaxFiles < 1 || c.MaxFiles > 500 {
		errs = append(errs, fmt.Errorf("capabilities max_files must be 1-500, got %d", c.MaxFiles))
	}
	return errs
}
