package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzCredentialsFile reads arbitrary credentials files. Every "$KEY" in the
// input becomes a real-shaped key, so broken files carry it on the very line
// a TOML error would quote: no error from List or Lookup may show the key or
// its body, and List never shows more than its last four characters.
func FuzzCredentialsFile(f *testing.F) {
	const key = "sk-ant-api03-Zq8xR2vT9mK4pL7nW3bY6cH1dF5gJ0sA"
	body := strings.TrimPrefix(key, "sk-ant-api03-")
	f.Add("[anthropic]\napi_key = \"sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n")
	f.Add("[openai]\napi_key = 7\n")
	f.Add("[[anthropic]]\n")
	f.Add("[anthropic]\napi_key = \"$KEY\"\n")
	f.Add("[anthropic]\napi_key = \"$KEY\n")
	f.Add("[anthropic]\napi_key = $KEY\n")
	f.Add("[anthropic]\napi_key = \"$KEY\"\n[anthropic]\napi_key = \"$KEY\"\n")
	f.Add("$KEY = 1\n")
	f.Fuzz(func(t *testing.T, data string) {
		data = strings.ReplaceAll(data, "$KEY", key)
		p := filepath.Join(t.TempDir(), "credentials.toml")
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		cf := File{Path: p, Env: func(string) string { return "" }}
		leaks := func(err error) bool {
			return err != nil && (strings.Contains(err.Error(), key) || strings.Contains(err.Error(), body))
		}
		for _, prov := range Providers {
			if _, _, err := cf.Lookup(prov); leaks(err) {
				t.Fatalf("Lookup(%s) error shows the key: %v", prov, err)
			}
		}
		sts, err := cf.List()
		if leaks(err) {
			t.Fatalf("List error shows the key: %v", err)
		}
		if err != nil {
			return
		}
		for _, s := range sts {
			// Last4 is "" or "…" plus the key's last four bytes, never more.
			if rest, ok := strings.CutPrefix(s.Last4, "…"); s.Last4 != "" && (!ok || len(rest) > 4) {
				t.Fatalf("List shows %q for %s: more than the last four characters", s.Last4, s.Provider)
			}
		}
	})
}
