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
	status, err := (Client{Retries: 2}).Do(ctx, "GET", srv.URL, nil, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 429 || status != 429 || time.Since(start) > time.Second {
		t.Fatalf("status %d err %v after %v: must fail fast with the 429 instead of sleeping past the deadline", status, err, time.Since(start))
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

func statusServer(status int, hdr map[string]string, calls *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
	}))
}

func TestHugeRetryAfterGivesUpAtOnce(t *testing.T) {
	for _, hdr := range []map[string]string{
		{"Retry-After": "86400"},
		{"Retry-After": "1e10"},
		{"Retry-After": "1e300"},
		{"Retry-After-Ms": "1e300"},
		{"Retry-After-Ms": "99999999999"},
	} {
		var n atomic.Int32
		srv := statusServer(429, hdr, &n)
		start := time.Now()
		status, err := (Client{Retries: 2}).Do(context.Background(), "GET", srv.URL, nil, nil)
		srv.Close()
		var se *StatusError
		if !errors.As(err, &se) || se.Status != 429 || status != 429 {
			t.Errorf("%v: status %d err %v", hdr, status, err)
		}
		if time.Since(start) > time.Second || n.Load() != 1 {
			t.Errorf("%v: took %v with %d calls, want an immediate give-up", hdr, time.Since(start), n.Load())
		}
	}
}

func TestNegativeOrNaNRetryAfterFallsBackToBackoff(t *testing.T) {
	for _, v := range []string{"-5", "NaN", "Inf"} {
		var n atomic.Int32
		srv := statusServer(503, map[string]string{"Retry-After": v}, &n)
		start := time.Now()
		_, err := (Client{Retries: 1, Base: 20 * time.Millisecond}).Do(context.Background(), "GET", srv.URL, nil, nil)
		srv.Close()
		if err == nil || n.Load() != 2 || time.Since(start) < 10*time.Millisecond {
			t.Errorf("%q: err %v calls %d after %v, want one paced retry", v, err, n.Load(), time.Since(start))
		}
	}
}

func TestRetryAfterHTTPDateIsHonored(t *testing.T) {
	var n atomic.Int32
	var first time.Time
	var gap time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			first = time.Now()
			w.Header().Set("Retry-After", time.Now().Add(2*time.Second).UTC().Format(http.TimeFormat))
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
	if gap < 900*time.Millisecond || gap > 3*time.Second {
		t.Fatalf("retried after %v, want ~1-2s", gap)
	}
}

func TestPastRetryAfterDateFallsBackToBackoff(t *testing.T) {
	var n atomic.Int32
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	srv := statusServer(503, map[string]string{"Retry-After": past}, &n)
	defer srv.Close()
	start := time.Now()
	_, err := (Client{Retries: 1, Base: time.Millisecond}).Do(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil || n.Load() != 2 || time.Since(start) > time.Second {
		t.Fatalf("err %v calls %d after %v", err, n.Load(), time.Since(start))
	}
}

func TestDeadlineGiveUpReturnsTheStatusError(t *testing.T) {
	var n atomic.Int32
	srv := statusServer(429, map[string]string{"Retry-After": "5"}, &n)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := (Client{Retries: 2}).Do(ctx, "GET", srv.URL, nil, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 429 || status != 429 {
		t.Fatalf("status %d err %v", status, err)
	}
}

func TestKeyHeaderIsNotForwardedOnCrossHostRedirect(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "" {
			leaked.Add(1)
		}
		w.WriteHeader(200)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	// Both listen on 127.0.0.1 with different ports, so the hosts differ.
	c := Client{Headers: map[string]string{"x-api-key": "sk-secret"}}
	status, err := c.Do(context.Background(), "GET", srv.URL, nil, nil)
	if leaked.Load() != 0 {
		t.Fatal("x-api-key reached the redirect target")
	}
	if err == nil || status != 307 {
		t.Fatalf("status %d err %v, want the redirect refused", status, err)
	}
}

func TestNetworkErrorIsRetried(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	var out map[string]any
	status, err := (Client{Retries: 1, Base: time.Millisecond}).Do(context.Background(), "GET", srv.URL, nil, &out)
	if err != nil || status != 200 || n.Load() != 2 {
		t.Fatalf("status %d err %v calls %d", status, err, n.Load())
	}
}

func TestNetworkErrorIsRedacted(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/?key=sk-or-v1-abcdef0123456789"
	srv.Close()
	_, err := (Client{Secret: "sk-or-v1-abcdef0123456789", Base: time.Millisecond}).Do(context.Background(), "GET", url, nil, nil)
	if err == nil || strings.Contains(err.Error(), "abcdef0123456789") {
		t.Fatalf("err %v", err)
	}
}

func TestOversizedResponseIsAnExplicitError(t *testing.T) {
	old := maxBody
	maxBody = 64
	defer func() { maxBody = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"x":"`+strings.Repeat("a", 200)+`"}`)
	}))
	defer srv.Close()
	var out map[string]string
	_, err := (Client{}).Do(context.Background(), "GET", srv.URL, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "response too large") {
		t.Fatalf("err %v", err)
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
