package main

import (
	"strings"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/pricing"
)

func TestBackendSetCachesPerKindAndRebuildsOnProviderChange(t *testing.T) {
	cfg := config.Default()
	set := newBackendSet(paths.New(t.TempDir(), t.TempDir(), "/tmp/h.sock"), func() config.Config { return cfg }, nil)
	a1, err := set.Get(backend.KindOllama)
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := set.Get(backend.KindOllama)
	if a1 != a2 {
		t.Fatal("same settings: cached adapter")
	}
	cfg.Providers = map[string]config.ProviderConfig{"ollama": {BaseURL: "http://gpu:11434"}}
	a3, _ := set.Get(backend.KindOllama)
	if a3 == a1 || set.OllamaURL() != "http://gpu:11434" {
		t.Fatal("changed [providers.ollama]: rebuilt")
	}
	for _, k := range []string{"claude-cli", "codex-cli", "anthropic-api", "openai-api", "openrouter", "ollama"} {
		if b, err := set.Get(k); err != nil || b.Kind() != k {
			t.Errorf("Get(%s) = %v %v", k, b, err)
		}
	}
}

func TestEstimateLine(t *testing.T) {
	tb := pricing.Default()
	d := dots.New("x")
	d.Backend = dots.BackendSpec{Kind: dots.KindAnthropicAPI}
	if l := estimateLine(tb, d); !strings.HasPrefix(l, "est. $") || !strings.Contains(l, "at 3 turns") {
		t.Fatalf("anthropic default model: %q", l)
	}
	d.Backend = dots.BackendSpec{Kind: dots.KindOpenAIAPI, Model: "gpt-unpriced"}
	if l := estimateLine(tb, d); !strings.HasPrefix(l, "price unknown") {
		t.Fatalf("unpriced: %q", l)
	}
	d.Backend = dots.BackendSpec{Kind: dots.KindOllama, Model: "m"}
	if l := estimateLine(tb, d); !strings.Contains(l, "$0") {
		t.Fatalf("ollama: %q", l)
	}
	d.Backend = dots.BackendSpec{Kind: "claude-cli"}
	if estimateLine(tb, d) != "" {
		t.Fatal("CLI dots: no line")
	}
}
