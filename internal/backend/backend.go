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
	Pass      string // PassReview or PassMemory

	// Phase 4: API backends. Zero values mean provider defaults.
	Effort        string // "" | low | medium | high | xhigh | max (anthropic-api, openai-api)
	MaxTokens     int    // 0 = DefaultMaxTokens
	BaseURL       string // ollama, openrouter: per-dot endpoint override
	ContextWindow int    // ollama num_ctx; 0 = DefaultContextWindow
	Fallbacks     string // anthropic-api: "" or "default" = on, "off"
}

// Pass values: a normal review, or an observe dot's memory-only pass.
const (
	PassReview = ""
	PassMemory = "memory"
)

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
