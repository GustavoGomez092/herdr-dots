package anthropicapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/platform"
)

// Ruling P10-R7 (S14): with no key in credentials.toml the adapter uses the
// SDK's own chain (ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN). Those values
// have no key shape Redact knows, so they are handed to Redact by value: an
// error that echoes them never shows them.
func TestSDKChainTokensAreRedactedFromErrors(t *testing.T) {
	const token, apiKey = "opaqueauthtokenvaluezz", "plainapikeyvaluewithoutshape"
	t.Setenv(platform.HomeEnv, t.TempDir())
	t.Setenv("ANTHROPIC_AUTH_TOKEN", token)
	t.Setenv("ANTHROPIC_API_KEY", apiKey)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad credentials %s / %s"}}`, token, apiKey)
	}))
	defer srv.Close()
	b := New(backend.Options{SDKCreds: true, Provider: config.ProviderConfig{BaseURL: srv.URL}})
	err := b.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rejected the request (400)") {
		t.Fatalf("err = %v, want the 400 refusal", err)
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), apiKey) {
		t.Fatalf("the SDK-chain credential reached the error: %v", err)
	}
	if _, key, cerr := b.client(); cerr != nil || key != "" {
		t.Fatalf("client() key = %q, %v: the SDK chain's values are never the returned key", key, cerr)
	}
}
