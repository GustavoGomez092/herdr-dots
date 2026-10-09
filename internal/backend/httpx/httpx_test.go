package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetriesOn429ThenSucceeds(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"q":1}` || r.Header.Get("X-Title") != "Herdr Dots" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request %s %v", body, r.Header)
		}
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After-Ms", "5")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	var out struct{ OK bool }
	c := Client{Retries: 2, Headers: map[string]string{"X-Title": "Herdr Dots"}, Base: time.Millisecond}
	status, err := c.Do(context.Background(), "POST", srv.URL, map[string]int{"q": 1}, &out)
	if err != nil || status != 200 || !out.OK || n.Load() != 2 {
		t.Fatalf("status %d err %v out %+v calls %d", status, err, out, n.Load())
	}
}

func TestRetryAfterSecondsIsHonored(t *testing.T) {
	var first time.Time
	var gap time.Duration
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			first = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(503)
			return
		}
		gap = time.Since(first)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	if _, err := (Client{Retries: 1, Base: time.Millisecond}).Do(context.Background(), "GET", srv.URL, nil, nil); err != nil {
		t.Fatal(err)
	}
	if gap < 900*time.Millisecond {
		t.Fatalf("retried after %v, want ~1s", gap)
	}
}

func TestGivesUpAfterRetriesWithCleanError(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	}))
	defer srv.Close()
	status, err := (Client{Retries: 2, Base: time.Millisecond}).Do(context.Background(), "GET", srv.URL, nil, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 429 || status != 429 || n.Load() != 3 {
		t.Fatalf("status %d err %v calls %d", status, err, n.Load())
	}
}

func TestStopsWhenTheWaitWouldPassTheDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := (Client{Retries: 2}).Do(ctx, "GET", srv.URL, nil, nil)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("err %v after %v: must fail fast instead of sleeping past the deadline", err, time.Since(start))
	}
}

func TestNoRetryOn4xxAndBodyIsRedacted(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"No auth credentials found for key sk-or-v1-abcdef0123456789"}}`)
	}))
	defer srv.Close()
	_, err := (Client{Retries: 2, Secret: "sk-or-v1-abcdef0123456789"}).Do(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil || strings.Contains(err.Error(), "abcdef0123456789") || n.Load() != 1 {
		t.Fatalf("err %v calls %d", err, n.Load())
	}
}

func TestConnectionRefusedIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	status, err := (Client{Base: time.Millisecond}).Do(context.Background(), "GET", url, nil, nil)
	if err == nil || status != 0 {
		t.Fatalf("status %d err %v", status, err)
	}
}
