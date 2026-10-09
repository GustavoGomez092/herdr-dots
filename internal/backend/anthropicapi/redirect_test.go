package anthropicapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
)

// Final review item 14: the SDK client refuses cross-host redirects, so
// x-api-key is never forwarded to another host.
func TestKeyIsNotForwardedOnCrossHostRedirect(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Any request here means the redirect was followed with the
		// configured headers (extra_headers included).
		leaked.Add(1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"has_more":false}`))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	b := New(backend.Options{Creds: staticCreds{"anthropic": testKey}, Provider: config.ProviderConfig{BaseURL: srv.URL}})
	_ = b.Check(context.Background())
	if leaked.Load() != 0 {
		t.Fatal("a cross-host redirect was followed: the key and extra headers reached another host")
	}
}
