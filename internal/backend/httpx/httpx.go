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
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"herdr-dots/internal/credentials"
)

// maxBody caps how much of a response is read.
// A var only so tests can shrink it.
var maxBody int64 = 8 << 20

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
		hc = sameHostClient
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
			data, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
			resp.Body.Close()
			if status >= 200 && status < 300 {
				if rerr != nil {
					return status, c.scrub(rerr)
				}
				if int64(len(data)) > maxBody {
					return status, fmt.Errorf("response too large (over %d bytes)", maxBody)
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
			var tooLong bool
			wait, tooLong = retryAfter(resp.Header, c.maxWait())
			if tooLong {
				// The server asked for longer than MaxWait: sleeping that
				// long would stall the run, so report the real status now.
				return status, lastErr
			}
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
	base, maxw := c.Base, c.maxWait()
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

// retryAfter reads Retry-After-Ms or Retry-After (seconds or HTTP date).
// It returns 0 when there is no usable hint (absent, NaN, Inf, negative or a
// past date), and tooLong when the hint exceeds maxw. The comparison is done
// on the float before any Duration conversion, so huge values cannot overflow.
func retryAfter(h http.Header, maxw time.Duration) (wait time.Duration, tooLong bool) {
	limit := maxw.Seconds()
	secs := func(v float64) (time.Duration, bool, bool) { // wait, tooLong, ok
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return 0, false, false
		}
		if v > limit {
			return 0, true, true
		}
		return time.Duration(v * float64(time.Second)), false, true
	}
	if v := strings.TrimSpace(h.Get("Retry-After-Ms")); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil {
			if d, long, ok := secs(ms / 1000); ok {
				return d, long
			}
		}
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil {
		d, long, _ := secs(s)
		return d, long
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			if d.Seconds() > limit {
				return 0, true
			}
			return d, false
		}
	}
	return 0, false
}

func (c Client) maxWait() time.Duration {
	if c.MaxWait <= 0 {
		return 8 * time.Second
	}
	return c.MaxWait
}

// sameHostClient is used when Client.HTTP is nil. It refuses redirects to a
// different host, so configured auth headers (x-api-key and friends) can never
// be forwarded to a third party. A caller-supplied HTTP client keeps its own
// policy.
var sameHostClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if req.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	},
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
