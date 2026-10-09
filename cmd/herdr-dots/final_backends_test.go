package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/paths"
)

// fakeOllama answers /api/version and /api/tags.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			fmt.Fprint(w, `{"version":"0.12.0"}`)
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"llama3:latest"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func deadURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

// setFor builds a backend set whose [providers.ollama] points at a dead
// server, so only the dot's own base_url can succeed.
func setFor(t *testing.T) *backendSet {
	t.Helper()
	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{"ollama": {BaseURL: deadURL(t)}}
	return newBackendSet(paths.New(t.TempDir(), t.TempDir(), "/tmp/h.sock"), func() config.Config { return cfg }, nil)
}

func ollamaDot(model, base string) dots.Dot {
	d := dots.New("Local")
	d.Purpose = "p"
	d.Backend = dots.BackendSpec{Kind: dots.KindOllama, Model: model, BaseURL: base, Args: []string{}}
	return d
}

// Final review item 2: the save check and the model picker use the dot's own
// base_url.
func TestCheckDotAndModelsUseTheDotsBaseURL(t *testing.T) {
	srv := fakeOllama(t)
	set := setFor(t)
	if warn, err := set.CheckDot(context.Background(), ollamaDot("llama3", srv.URL)); err != nil || warn != "" {
		t.Fatalf("remote base_url: warn %q err %v", warn, err)
	}
	if _, err := set.CheckDot(context.Background(), ollamaDot("mistral", srv.URL)); err == nil || !strings.Contains(err.Error(), "not pulled") {
		t.Fatalf("a real validation error still blocks: %v", err)
	}
	ids, err := set.Models(context.Background(), dots.KindOllama, srv.URL)
	if err != nil || len(ids) != 1 || ids[0] != "llama3:latest" {
		t.Fatalf("models %v %v", ids, err)
	}
}

// Final review item 2: an unreachable server is a warning; the save goes on.
func TestCheckDotConnectionRefusedIsAWarning(t *testing.T) {
	set := setFor(t)
	warn, err := set.CheckDot(context.Background(), ollamaDot("llama3", deadURL(t)))
	if err != nil {
		t.Fatalf("connection refused must not block the save: %v", err)
	}
	if !strings.Contains(warn, "isn't running") || !strings.Contains(warn, "not checked") {
		t.Fatalf("warning %q", warn)
	}
}
