package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"herdr-dots/internal/platform"
	"herdr-dots/internal/pricing"
	"herdr-dots/internal/textutil"
)

// Backend kinds (a dot's [backend].kind).
const (
	KindClaudeCLI    = "claude-cli"
	KindCodexCLI     = "codex-cli"
	KindAnthropicAPI = "anthropic-api"
	KindOpenAIAPI    = "openai-api"
	KindOpenRouter   = "openrouter"
	KindOllama       = "ollama"
)

// Defaults shared by the API adapters.
const (
	DefaultMaxTokens     = 4096
	DefaultContextWindow = 16384
)

// ProviderFor maps a kind to its credentials/pricing/config provider name
// ("" for CLI kinds).
func ProviderFor(kind string) string {
	switch kind {
	case KindAnthropicAPI:
		return "anthropic"
	case KindOpenAIAPI:
		return "openai"
	case KindOpenRouter:
		return "openrouter"
	case KindOllama:
		return "ollama"
	}
	return ""
}

// ModelLister is implemented by backends that can list their models (the
// editor's ctrl+space picker). r carries the dot's settings; adapters use
// r.BaseURL (ollama, openrouter) so a dot's own server is the one asked.
type ModelLister interface {
	Models(ctx context.Context, r Request) ([]string, error)
}

// ModelChecker is implemented by backends that can confirm one model
// (r.Model) is usable with the current credentials at the dot's endpoint
// (r.BaseURL). The editor runs it on save.
type ModelChecker interface {
	CheckModel(ctx context.Context, r Request) error
}

// ErrUnreachable marks a provider that could not be reached at all
// (connection refused). The editor's save check treats it, and a timeout, as
// a warning rather than a validation failure.
var ErrUnreachable = errors.New("provider unreachable")

// MarkUnreachable returns err unchanged in text, but errors.Is(…,
// ErrUnreachable) reports true.
func MarkUnreachable(err error) error {
	if err == nil {
		return nil
	}
	return unreachable{err}
}

type unreachable struct{ error }

func (u unreachable) Is(target error) bool { return target == ErrUnreachable }
func (u unreachable) Unwrap() error        { return u.error }

// Unreachable reports whether err means the provider never answered:
// connection refused or a timeout. Validation errors (bad key, unknown
// model) are not.
func Unreachable(err error) bool {
	return errors.Is(err, ErrUnreachable) || errors.Is(err, context.DeadlineExceeded) || platform.ConnRefused(err)
}

// ErrRefused marks a provider safety refusal. It is a run error: it reaches
// the feed and counts toward the failure stop.
var ErrRefused = errors.New("refused")

// Refused is the run error for a refusal: "refused (cyber)". category is
// provider text (a category or the refusal itself): control characters are
// stripped, since the error reaches the engine log and the feed.
func Refused(category string) error {
	category = strings.Join(strings.Fields(textutil.SanitizeModelText(category)), " ")
	if category == "" {
		category = "unspecified"
	}
	return fmt.Errorf("%w (%s)", ErrRefused, category)
}

// MaxTokensFor is r.MaxTokens or DefaultMaxTokens.
func MaxTokensFor(r Request) int {
	if r.MaxTokens > 0 {
		return r.MaxTokens
	}
	return DefaultMaxTokens
}

// APIRun is what an API adapter's tool loop produced.
type APIRun struct {
	Text      string
	Usage     pricing.Usage
	CostUSD   float64
	CostKnown bool
	Model     string
	ToolCalls int
	Capped    bool
	Started   time.Time
}

// FinishAPI turns an API run into a Result: tokens, cost and provenance, then
// the reply parsed exactly like the CLI backends parse theirs (verdict plus
// memory ops, or memory-pass ops).
func FinishAPI(r Request, a APIRun) (Result, error) {
	res := PartialAPI(a)
	res.Raw = a.Text
	return finishResult(res, a.Text, r.Pass)
}

// PartialAPI is the Result for a run that failed part way: the spend so far
// and no verdict, so the engine still records tokens and cost.
func PartialAPI(a APIRun) Result {
	res := Result{
		InputTokens:      a.Usage.In + a.Usage.CacheRead + a.Usage.CacheWrite,
		OutputTokens:     a.Usage.Out,
		CacheReadTokens:  a.Usage.CacheRead,
		CacheWriteTokens: a.Usage.CacheWrite,
		CostUSD:          a.CostUSD,
		CostKnown:        a.CostKnown,
		Model:            a.Model,
		ToolCalls:        a.ToolCalls,
		ToolsCapped:      a.Capped,
	}
	if !a.Started.IsZero() {
		res.Duration = time.Since(a.Started)
	}
	return res
}

// ValidReply reports whether text parses as the reply r expects.
func ValidReply(r Request, text string) bool {
	_, err := finishResult(Result{}, text, r.Pass)
	return err == nil
}
