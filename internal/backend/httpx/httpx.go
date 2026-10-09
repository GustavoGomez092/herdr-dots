// Package httpx is the small JSON-over-HTTP client the hand-rolled adapters
// (OpenRouter, Ollama) share: bounded retries with backoff that honors
// Retry-After, a capped response body, and errors scrubbed of secrets.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"herdr-dots/internal/credentials"
)

// maxBody caps how much of a response is read.
const maxBody = 8 << 20

// StatusError is a non-2xx response. Body is already redacted and clipped.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// Client sends JSON requests.
type Client struct {
	HTTP    *http.Client      // nil → http.DefaultClient
	Headers map[string]string // sent on every request
	Retries int               // extra attempts on 408/409/429/5xx and network errors
	Secret  string            // redacted from every error
	Base    time.Duration     // first backoff; 0 → 500ms
	MaxWait time.Duration     // backoff cap; 0 → 8s
}

// Retryable reports whether a status is worth retrying.
func Retryable(status int) bool {
	return status == 408 || status == 409 || status == 429 || status >= 500
}

// Do sends method url with body (JSON, nil = none) and decodes a 2xx response
// into out (nil = discard). It returns the last status seen (0 when no
// response arrived).
func (c Client) Do(ctx context.Context, method, url string, body, out any) (int, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		payload = b
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	var lastErr error
	status := 0
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, rd)
		if err != nil {
			return 0, c.scrub(err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range c.Headers {
			req.Header.Set(k, v)
		}
		resp, err := hc.Do(req)
		var wait time.Duration
		if err != nil {
			if ctx.Err() != nil {
				return status, ctx.Err()
			}
			lastErr = c.scrub(err)
		} else {
			status = resp.StatusCode
			data, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
			resp.Body.Close()
			if status >= 200 && status < 300 {
				if rerr != nil {
					return status, c.scrub(rerr)
				}
				if out == nil || len(bytes.TrimSpace(data)) == 0 {
					return status, nil
				}
				if err := json.Unmarshal(data, out); err != nil {
					return status, fmt.Errorf("decode response: %w", err)
				}
				return status, nil
			}
			lastErr = &StatusError{Status: status, Body: clip(credentials.Redact(string(data), c.Secret), 500)}
			if !Retryable(status) {
				return status, lastErr
			}
			wait = retryAfter(resp.Header)
		}
		if attempt >= c.Retries {
			return status, lastErr
		}
		if wait == 0 {
			wait = c.backoff(attempt)
		}
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) < wait {
			return status, lastErr // the run would time out while waiting
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return status, ctx.Err()
		case <-t.C:
		}
	}
}

func (c Client) backoff(attempt int) time.Duration {
	base, maxw := c.Base, c.MaxWait
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	if maxw <= 0 {
		maxw = 8 * time.Second
	}
	d := base << attempt
	if d > maxw || d <= 0 {
		d = maxw
	}
	return d/2 + rand.N(d/2+1) // jitter in [d/2, d]
}

// retryAfter reads retry-after-ms or Retry-After (seconds or HTTP date).
func retryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil && ms >= 0 {
			return time.Duration(ms * float64(time.Millisecond))
		}
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil && s >= 0 {
		return time.Duration(s * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func (c Client) scrub(err error) error {
	if err == nil {
		return nil
	}
	var se *StatusError
	if errors.As(err, &se) {
		return err
	}
	return errors.New(credentials.Redact(err.Error(), c.Secret))
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
