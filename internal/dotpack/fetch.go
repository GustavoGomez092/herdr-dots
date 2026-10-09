package dotpack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"herdr-dots/internal/backend/httpx"
	"herdr-dots/internal/memory"
)

// fetchTimeout bounds a download when the client sets no timeout.
const fetchTimeout = 30 * time.Second

var errUserInfo = errors.New("dot pack URLs can't carry a user name or password")

// Fetch downloads a pack over https (every redirect must stay https and on the
// same host, at most five), refuses anything but a 200 with a text (or
// application/toml) body, and reads at most maxBytes. It sends no
// credentials: URLs with a user name or password are refused and the
// client's cookie jar is not used. It returns the body and the final URL
// after redirects, which the preview shows. The body is untrusted data,
// never run; there are no automatic updates.
func Fetch(ctx context.Context, c *http.Client, rawURL string, maxBytes int64) ([]byte, string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, "", ErrNotHTTPS
	}
	if u.User != nil {
		return nil, "", errUserInfo
	}
	if c == nil {
		c = http.DefaultClient
	}
	cl := *c
	cl.Jar = nil
	if cl.Timeout == 0 {
		cl.Timeout = fetchTimeout
	}
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return ErrNotHTTPS
		}
		if req.URL.User != nil {
			return errUserInfo
		}
		if len(via) >= 5 {
			return errors.New("more than 5 redirects")
		}
		if err := httpx.SameHostRedirect(req, via); err != nil {
			if errors.Is(err, http.ErrUseLastResponse) {
				return errors.New("a redirect to another host was refused")
			}
			return err
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := cl.Do(req)
	if err != nil {
		if errors.Is(err, ErrNotHTTPS) {
			return nil, "", fmt.Errorf("%w: a redirect left https", ErrNotHTTPS)
		}
		return nil, "", err
	}
	defer resp.Body.Close()
	final := resp.Request.URL.Redacted()
	if resp.StatusCode != http.StatusOK {
		return nil, final, fmt.Errorf("%s: HTTP %d", final, resp.StatusCode)
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(mt, "text/") && mt != "application/toml" {
		return nil, final, fmt.Errorf("%w (%q from %s)", ErrContentType, mt, final)
	}
	if resp.ContentLength > maxBytes {
		return nil, final, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, final, err
	}
	if int64(len(data)) > maxBytes {
		return nil, final, ErrTooLarge
	}
	return data, final, nil
}

// Memory choices for an export (spec S9).
const (
	WithDot        = "dot"         // the dot's own memory for the project
	WithDotProject = "dot+project" // plus the project's shared memory
)

// SelectMemory picks what an export may carry: active entries of the
// ImportKinds only, the dot's scope (and with dot+project the project's),
// never global memory or proposals, and the developer's own entries only with
// includeUser. with "" means no memory.
func SelectMemory(st *memory.Store, project, slug, with string, includeUser bool) ([]memory.Entry, error) {
	if with == "" {
		return nil, nil
	}
	if with != WithDot && with != WithDotProject {
		return nil, fmt.Errorf("--with-memory %q: want %s or %s", with, WithDot, WithDotProject)
	}
	if st == nil {
		return nil, errors.New("memory is turned off (memory_enabled = false)")
	}
	if project == "" {
		return nil, errors.New("no project: run this in the repository or pass --project")
	}
	active := []memory.Status{memory.StatusActive}
	es, err := st.List(project, memory.Filter{Scope: memory.ScopeDot, Dot: slug, Statuses: active, Kinds: memory.ImportKinds})
	if err != nil {
		return nil, err
	}
	if with == WithDotProject {
		pe, err := st.List(project, memory.Filter{Scope: memory.ScopeProject, Statuses: active, Kinds: memory.ImportKinds})
		if err != nil {
			return nil, err
		}
		es = append(pe, es...)
	}
	out := es[:0]
	for _, e := range es {
		if e.Source == memory.SourceUser && !includeUser {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
