package anthropicapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/platform"
)

// The SDK chain also reads a federation identity token and extra headers
// (which can carry a gateway key); both are handed to Redact by value. A
// credentials.toml key means the SDK chain is off: only that key is known.
func TestSDKChainKnownSecretsCoverHeadersAndIdentityToken(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_IDENTITY_TOKEN", "federationidentitytokenzz")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Gateway-Key: gatewaysecretvaluezz\nX-Team: platform")
	got := (&Backend{o: backend.Options{SDKCreds: true}}).known("")
	for _, want := range []string{"federationidentitytokenzz", "gatewaysecretvaluezz"} {
		if !slices.Contains(got, want) {
			t.Errorf("known() = %q, lacks %q", got, want)
		}
	}
	if got := (&Backend{o: backend.Options{SDKCreds: true}}).known("credkey123456"); !slices.Equal(got, []string{"credkey123456"}) {
		t.Errorf("with a credentials key: known() = %q", got)
	}
}

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

// S23 (post-rc hardening, the cheap part): the federation identity token can
// come from a file (ANTHROPIC_IDENTITY_TOKEN_FILE); its content is handed to
// Redact by value like the environment variable's.
func TestSDKChainKnownSecretsCoverTheIdentityTokenFile(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_IDENTITY_TOKEN", "")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "")
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  identitytokenfromfilezz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_IDENTITY_TOKEN_FILE", path)
	if got := (&Backend{o: backend.Options{SDKCreds: true}}).known(""); !slices.Contains(got, "identitytokenfromfilezz") {
		t.Fatalf("known() = %q, lacks the token file's content", got)
	}
	t.Setenv("ANTHROPIC_IDENTITY_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	if got := (&Backend{o: backend.Options{SDKCreds: true}}).known(""); len(got) != 0 {
		t.Fatalf("a missing token file adds nothing: %q", got)
	}
}
