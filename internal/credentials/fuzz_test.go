package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzCredentialsFile(f *testing.F) {
	f.Add("[anthropic]\napi_key = \"sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n")
	f.Add("[openai]\napi_key = 7\n")
	f.Add("[[anthropic]]\n")
	f.Fuzz(func(t *testing.T, data string) {
		p := filepath.Join(t.TempDir(), "credentials.toml")
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		sts, err := File{Path: p, Env: func(string) string { return "" }}.List()
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
