package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform"
)

// Final review item 15: a deadline that hits while the body is read stays
// errors.Is(context.DeadlineExceeded) after scrubbing, and the secret is
// still redacted.
func TestDeadlineMidBodyKeepsDeadlineExceeded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"partial":`))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var out map[string]any
	_, err := (Client{Secret: "sk-or-v1-abcdef0123456789"}).Do(ctx, "GET", srv.URL+"/?k=sk-or-v1-abcdef0123456789", nil, &out)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v (%T): want errors.Is DeadlineExceeded", err, err)
	}
	if strings.Contains(err.Error(), "abcdef0123456789") {
		t.Fatalf("secret leaked: %v", err)
	}
}

// Scrubbing keeps connection refused recognisable (the editor's save check
// treats it as "unreachable", final review item 2).
func TestScrubKeepsConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := (Client{Base: time.Millisecond}).Do(context.Background(), "GET", url, nil, nil)
	if !platform.ConnRefused(err) {
		t.Fatalf("err %v: want a connection refusal (platform.ConnRefused)", err)
	}
}
