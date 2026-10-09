package dots

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"slices"
	"strings"
)

// API backend kinds (Phase 4). The CLI kinds stay first in BackendKinds.
const (
	KindAnthropicAPI = "anthropic-api"
	KindOpenAIAPI    = "openai-api"
	KindOpenRouter   = "openrouter"
	KindOllama       = "ollama"
)

// Efforts are the [backend] effort values ("" = provider default).
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// FallbackModes are anthropic-api's [backend] fallbacks values ("" = default).
var FallbackModes = []string{"default", "off"}

// DefaultAnthropicModel is the anthropic-api model when none is set.
const DefaultAnthropicModel = "claude-opus-5-5"

const (
	maxTokensLimit = 64000
	minContext     = 2048
	maxContext     = 262144
)

// IsAPIKind reports whether kind talks to an API instead of a CLI.
func IsAPIKind(kind string) bool {
	return kind == KindAnthropicAPI || kind == KindOpenAIAPI || kind == KindOpenRouter || kind == KindOllama
}

// DefaultModelFor is the model the editor fills in when the kind changes.
func DefaultModelFor(kind string) string {
	switch kind {
	case "claude-cli":
		return "sonnet"
	case KindAnthropicAPI:
		return DefaultAnthropicModel
	}
	return ""
}

// loopback reports whether host is this machine (localhost or a loopback IP).
func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateAPI checks the Phase 4 backend and budget fields against the kind.
func validateAPI(d Dot) []error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	b, kind := d.Backend, d.Backend.Kind
	if IsAPIKind(kind) {
		if len(b.Args) > 0 {
			add("backend args are for CLI backends only; remove them for %s", kind)
		}
		if strings.TrimSpace(b.Model) == "" && kind != KindAnthropicAPI {
			add("backend model is required for %s", kind)
		}
	}
	if b.Effort != "" {
		if !slices.Contains(Efforts, b.Effort) {
			add("backend effort %q must be one of %v", b.Effort, Efforts)
		} else if kind != KindAnthropicAPI && kind != KindOpenAIAPI {
			add("backend effort applies to anthropic-api and openai-api only")
		}
	}
	if b.MaxTokens != 0 {
		if !IsAPIKind(kind) {
			add("backend max_tokens applies to API backends only")
		} else if b.MaxTokens < 1 || b.MaxTokens > maxTokensLimit {
			add("backend max_tokens must be 1-%d, got %d", maxTokensLimit, b.MaxTokens)
		}
	}
	if b.BaseURL != "" {
		if kind != KindOllama && kind != KindOpenRouter {
			add("backend base_url applies to ollama and openrouter only")
		} else if u, err := url.Parse(b.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			add("backend base_url %q must be an http(s) URL", b.BaseURL)
		} else if u.User != nil {
			add("backend base_url must not contain credentials")
		} else if kind != KindOllama && u.Scheme != "https" && !loopback(u.Hostname()) {
			// The API key travels in a header: never in clear text off the machine.
			add("backend base_url for %s must use https (plain http only for localhost)", kind)
		}
	}
	if b.ContextWindow != 0 {
		if kind != KindOllama {
			add("backend context_window applies to ollama only")
		} else if b.ContextWindow < minContext || b.ContextWindow > maxContext {
			add("backend context_window must be %d-%d, got %d", minContext, maxContext, b.ContextWindow)
		}
	}
	if b.Fallbacks != "" {
		if kind != KindAnthropicAPI {
			add("backend fallbacks applies to anthropic-api only")
		} else if !slices.Contains(FallbackModes, b.Fallbacks) {
			add("backend fallbacks %q must be one of %v", b.Fallbacks, FallbackModes)
		}
	}
	if usd := d.Budget.MaxUSDPerSession; math.IsNaN(usd) || math.IsInf(usd, 0) || usd < 0 {
		add("budget max_usd_per_session must be >= 0 (0 = off)")
	}
	return errs
}
