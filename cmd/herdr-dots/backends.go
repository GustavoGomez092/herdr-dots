package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"herdr-dots/internal/backend"
	_ "herdr-dots/internal/backend/all" // registers the API backends
	"herdr-dots/internal/backend/ollama"
	"herdr-dots/internal/config"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/pricing"
)

// backendSet builds and caches one backend per kind for the engine and the
// manage popup. config.toml is re-read on every Get (it can change at
// runtime); an adapter is rebuilt only when its provider settings change.
type backendSet struct {
	p      paths.Paths
	cfg    func() config.Config
	logf   func(string, ...any)
	runner backend.Runner
	sdk    bool // allow the Anthropic SDK credential chain

	mu    sync.Mutex
	cache map[string]cachedBackend
}

type cachedBackend struct {
	be  backend.Backend
	sig backendSig
}

type backendSig struct {
	Provider config.ProviderConfig
	Deny     []string
	EnvNames map[string]string
	Prices   string
}

func newBackendSet(p paths.Paths, cfg func() config.Config, logf func(string, ...any)) *backendSet {
	return &backendSet{p: p, cfg: cfg, logf: logf, runner: backend.ExecRunner{}, sdk: true, cache: map[string]cachedBackend{}}
}

// configLoader re-reads config.toml; a broken file yields defaults (the
// engine logs the error).
func configLoader(path string) func() config.Config { return configLoaderLogged(path, nil) }

// configLoaderLogged also logs one line (never the file's content) when
// config.toml is malformed and defaults are used.
func configLoaderLogged(path string, logf func(string, ...any)) func() config.Config {
	var mu sync.Mutex
	logged := false // one line per bad stretch, not one per run
	return func() config.Config {
		c, err := config.Load(path)
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			logged = false
		} else if !logged && logf != nil {
			logged = true
			logf("config.toml is malformed: provider settings fell back to defaults")
		}
		return c
	}
}

func (s *backendSet) creds(c config.Config) credentials.File {
	names := map[string]string{}
	for name, pc := range c.Providers {
		if pc.APIKeyEnv != "" {
			names[name] = pc.APIKeyEnv
		}
	}
	return credentials.File{Path: s.p.CredentialsFile(), EnvNames: names}
}

func (s *backendSet) pricesPath(c config.Config) string {
	if c.PricesFile != "" {
		return c.PricesFile
	}
	return s.p.PricesFile()
}

// Prices loads the price table (embedded + override), logging a bad override.
func (s *backendSet) Prices() pricing.Table {
	t, err := pricing.Load(s.pricesPath(s.cfg()))
	if err != nil && s.logf != nil {
		s.logf("prices: %v (using the embedded table)", err)
	}
	return t
}

// Get returns the backend for kind.
func (s *backendSet) Get(kind string) (backend.Backend, error) {
	c := s.cfg()
	provider := backend.ProviderFor(kind)
	creds := s.creds(c)
	sig := backendSig{Provider: c.Provider(provider), Deny: c.ToolDenyGlobs, EnvNames: creds.EnvNames, Prices: s.pricesPath(c)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cb, ok := s.cache[kind]; ok && reflect.DeepEqual(cb.sig, sig) {
		return cb.be, nil
	}
	prices, err := pricing.Load(sig.Prices)
	if err != nil && s.logf != nil {
		s.logf("prices: %v (using the embedded table)", err)
	}
	be, err := backend.NewWith(kind, backend.Options{
		Runner: s.runner, Creds: creds, Prices: prices, Provider: sig.Provider,
		Deny: sig.Deny, SDKCreds: s.sdk && kind == backend.KindAnthropicAPI, Logf: s.logf,
	})
	if err != nil {
		return nil, err
	}
	s.cache[kind] = cachedBackend{be: be, sig: sig}
	return be, nil
}

// CheckDot is the editor's save check: the CLI is installed, or the key works
// and the model is available (or pulled).
func (s *backendSet) CheckDot(ctx context.Context, d dots.Dot) error {
	be, err := s.Get(d.Backend.Kind)
	if err != nil {
		return err
	}
	if mc, ok := be.(backend.ModelChecker); ok {
		return mc.CheckModel(ctx, d.Backend.Model)
	}
	return be.Check(ctx)
}

// Models lists models for the editor's picker.
func (s *backendSet) Models(ctx context.Context, kind string) ([]string, error) {
	be, err := s.Get(kind)
	if err != nil {
		return nil, err
	}
	ml, ok := be.(backend.ModelLister)
	if !ok {
		return nil, fmt.Errorf("%s can't list models: type the model name", kind)
	}
	return ml.Models(ctx)
}

// kindForProvider maps a credentials provider to its backend kind.
var kindForProvider = map[string]string{
	"anthropic": backend.KindAnthropicAPI, "openai": backend.KindOpenAIAPI,
	"openrouter": backend.KindOpenRouter, "ollama": backend.KindOllama,
}

// TestProvider validates a provider's key (Ollama: the server answers).
// Only stored or env keys count: the SDK chain is not consulted.
func (s *backendSet) TestProvider(ctx context.Context, provider string) error {
	kind, ok := kindForProvider[provider]
	if !ok {
		return errors.New("unknown provider")
	}
	c := s.cfg()
	be, err := backend.NewWith(kind, backend.Options{Creds: s.creds(c), Provider: c.Provider(provider), Logf: s.logf})
	if err != nil {
		return err
	}
	return be.Check(ctx)
}

// OllamaURL is the server the ollama backend would use.
func (s *backendSet) OllamaURL() string {
	return ollama.New(backend.Options{Provider: s.cfg().Provider("ollama")}).BaseURL(backend.Request{})
}

// estimateLine is the editor's cost line for a dot.
func estimateLine(t pricing.Table, d dots.Dot) string {
	switch d.Backend.Kind {
	case dots.KindOllama:
		return "local model: $0 per run"
	case dots.KindOpenRouter:
		return "cost comes from OpenRouter after each run"
	case dots.KindAnthropicAPI, dots.KindOpenAIAPI:
	default:
		return ""
	}
	model := d.Backend.Model
	if model == "" && d.Backend.Kind == dots.KindAnthropicAPI {
		model = dots.DefaultAnthropicModel
	}
	usd, ok := t.Estimate(backend.ProviderFor(d.Backend.Kind), model, d.Context.Turns)
	if !ok {
		return "price unknown: dollar budgets won't count this dot (add it to prices.toml)"
	}
	return fmt.Sprintf("est. $%.4f/run at %d turns (prices as of %s)", usd, d.Context.Turns, t.AsOf)
}
