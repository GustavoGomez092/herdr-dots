// Package backend runs a dot against a model. v1 ships CLI backends (claude,
// codex) that reuse the user's existing logins; API and Ollama backends plug in
// behind the same interface in a later increment.
package backend

import (
	"context"

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
}

type Result struct {
	Verdict      Verdict
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Raw          string
}

type Backend interface {
	Kind() string
	Check(ctx context.Context) error
	Run(ctx context.Context, r Request) (Result, error)
}
