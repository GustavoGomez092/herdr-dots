// Package backend runs a dot against a model: CLI backends (claude, codex)
// that reuse the user's logins, and API backends (Anthropic, OpenAI,
// OpenRouter, Ollama) that live in sub-packages and register themselves.
package backend

import (
	"context"
	"time"

	"herdr-dots/internal/transcript"
)

type Verdict struct {
	Relevant bool
	Severity string // info | warn | critical
	Message  string
}

type Request struct {
	DotName   string
	Purpose   string
	Verbosity string
	Model     string
	Args      []string
	RepoRead  bool
	Cwd       string
	Turns     []transcript.Turn
	Question  string // manual summon only
	Force     bool   // summoned: must answer
	Memory    string // pre-rendered memory block (memory.Render); "" = none
	Learn     bool   // the dot may return memory ops in its reply
	Pass      string // PassReview, PassMemory, PassWrite or PassResearch
	Web       bool   // research only: add web search/fetch where the backend has them (WebSupported)
	Trigger   string // what started a review: TriggerTurn, TriggerScheduled or TriggerCommit
	// Write gives the run file-editing tools confined to Cwd (an isolated
	// worktree). Backends honor it only together with Pass == PassWrite, so
	// one stray field can never make a review write.
	Write bool

	// Phase 4: API backends. Zero values mean provider defaults.
	Effort        string // "" | low | medium | high | xhigh | max (anthropic-api, openai-api)
	MaxTokens     int    // 0 = DefaultMaxTokens
	BaseURL       string // ollama, openrouter: per-dot endpoint override
	ContextWindow int    // ollama num_ctx; 0 = DefaultContextWindow
	Fallbacks     string // anthropic-api: "" or "default" = on, "off"
}

// Pass values: a normal review, an observe dot's memory-only pass, or a
// draft run that edits an isolated copy of the repository (Phase 7), or an
// idle research pass (Phase 8: read-only, like a review).
const (
	PassReview   = ""
	PassMemory   = "memory"
	PassWrite    = "write"
	PassResearch = "research"
)

// Trigger values for a review (Phase 8). They only change how the user
// prompt labels Question.
const (
	TriggerTurn      = ""          // a finished turn, a summon or a council
	TriggerScheduled = "scheduled" // a [schedule] slot: Question is the standing task
	TriggerCommit    = "commit"    // a new commit: Question is the commit message, stat and patch
)

// WriteMode reports whether r is a draft run allowed to edit files.
func WriteMode(r Request) bool { return r.Write && r.Pass == PassWrite }

// Model-written memory ops kept per reply.
const (
	MaxReviewOps = 3
	MaxPassOps   = 5
)

// MemoryOp is one memory change a model asks for. Op is add, update or
// forget; Scope is dot or project; ID is a display ref like d:m9.
type MemoryOp struct {
	Op    string   `json:"op"`
	ID    string   `json:"id,omitempty"`
	Scope string   `json:"scope,omitempty"`
	Kind  string   `json:"kind,omitempty"`
	Text  string   `json:"text,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

type Result struct {
	Verdict      Verdict
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Raw          string
	Remember     []MemoryOp // memory ops from the reply; Verdict stays comparable

	// Phase 4: usage detail and cost provenance.
	CacheReadTokens  int
	CacheWriteTokens int
	CostKnown        bool          // CostUSD came from the provider or the price table
	Model            string        // the model that answered (differs from Request.Model after a fallback)
	ToolCalls        int           // repo tool calls made by an API dot
	ToolsCapped      bool          // the tool-call cap was reached
	Duration         time.Duration // wall time of the run
}

type Backend interface {
	Kind() string
	Check(ctx context.Context) error
	Run(ctx context.Context, r Request) (Result, error)
}
