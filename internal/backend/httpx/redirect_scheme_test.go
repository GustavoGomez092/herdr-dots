package httpx

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// Ruling P10-R7 (S15): a redirect that keeps the host but drops TLS
// (https → http) would send the key in clear text; it is refused like a
// cross-host one. Same scheme on the same host still follows.
func TestSameHostRedirectRefusesASchemeChange(t *testing.T) {
	req := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Request{URL: u}
	}
	from := []*http.Request{req("https://api.example.com/v1/messages")}
	if err := SameHostRedirect(req("http://api.example.com/v1/messages"), from); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("https→http on the same host: %v, want http.ErrUseLastResponse", err)
	}
	if err := SameHostRedirect(req("https://api.example.com/v2/messages"), from); err != nil {
		t.Fatalf("same scheme, same host: %v", err)
	}
	if err := SameHostRedirect(req("https://other.example.com/v1"), from); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("cross-host: %v", err)
	}
}
