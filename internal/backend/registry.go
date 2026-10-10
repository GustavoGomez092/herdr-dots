package backend

import (
	"fmt"
	"net/http"
	"sort"
	"sync"

	"herdr-dots/internal/config"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/pricing"
)

// Options carries everything an adapter needs. The engine builds one per
// kind (cmd/herdr-dots/backends.go); tests pass fakes.
type Options struct {
	Runner   Runner                // CLI backends
	Creds    credentials.Source    // API keys; nil = none
	HTTP     *http.Client          // nil = adapter default (no client-wide timeout; ctx bounds runs)
	Prices   pricing.Table         // zero value = pricing.Default()
	Provider config.ProviderConfig // [providers.<name>] for this kind's provider
	Deny     []string              // extra tool deny globs (config tool_deny_globs)
	SDKCreds bool                  // anthropic-api only: fall back to the SDK credential chain (never in tests)
	Logf     func(format string, args ...any)
}

// PriceTable returns o.Prices, or the embedded table when it is empty.
func (o Options) PriceTable() pricing.Table {
	if o.Prices.Prices == nil {
		return pricing.Default()
	}
	return o.Prices
}

// Log formats through o.Logf when set.
func (o Options) Log(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Factory builds a backend for one kind.
type Factory func(o Options) (Backend, error)

var (
	regMu     sync.RWMutex
	factories = map[string]Factory{}
)

// Register makes a kind available to NewWith. Adapter packages call it from
// init(), so this package never imports them (no import cycle). Registering
// a kind twice, or a nil factory, is a programming error and panics.
func Register(kind string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	if f == nil {
		panic("backend: nil factory for " + kind)
	}
	if _, dup := factories[kind]; dup {
		panic("backend: kind registered twice: " + kind)
	}
	factories[kind] = f
}

// Kinds lists the registered kinds, sorted.
func Kinds() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// NewWith returns the backend for a dot's [backend].kind.
func NewWith(kind string, o Options) (Backend, error) {
	regMu.RLock()
	f := factories[kind]
	regMu.RUnlock()
	if f == nil {
		return nil, fmt.Errorf("unknown backend %q", kind)
	}
	return f(o)
}

// New is v1's constructor, kept for callers that only run CLI dots. API kinds
// built this way have no credentials and fail Check.
func New(kind string, r Runner) (Backend, error) { return NewWith(kind, Options{Runner: r}) }

func init() {
	Register(KindClaudeCLI, func(o Options) (Backend, error) { return ClaudeCLI{Runner: o.Runner, Deny: o.Deny}, nil })
	Register(KindCodexCLI, func(o Options) (Backend, error) { return CodexCLI{Runner: o.Runner}, nil })
}
