package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func credFile(t *testing.T) File {
	t.Helper()
	return File{Path: filepath.Join(t.TempDir(), "credentials.toml"), Env: envOf(nil)}
}

func TestSetWritesOwnerOnlyAndLookupReadsIt(t *testing.T) {
	f := credFile(t)
	if err := f.Set("anthropic", "  sk-ant-api03-abcdefghijklmnop  "); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(f.Path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err %v, want 0600", st.Mode().Perm(), err)
	}
	key, from, err := f.Lookup("anthropic")
	if err != nil || key != "sk-ant-api03-abcdefghijklmnop" || from != "file" {
		t.Fatalf("Lookup = %q %q %v", key, from, err)
	}
	data, _ := os.ReadFile(f.Path)
	if !strings.HasPrefix(string(data), "# Managed by herdr-dots") {
		t.Fatalf("missing header:\n%s", data)
	}
}

func TestEnvBeatsFileAndConfigCanRenameTheVariable(t *testing.T) {
	f := credFile(t)
	if err := f.Set("openai", "sk-file-0000000000000000"); err != nil {
		t.Fatal(err)
	}
	f.Env = envOf(map[string]string{"OPENAI_API_KEY": "sk-env-1111111111111111", "WORK_OPENAI": "sk-work-2222222222222222"})
	if k, from, _ := f.Lookup("openai"); k != "sk-env-1111111111111111" || from != "env:OPENAI_API_KEY" {
		t.Fatalf("default env: %q %q", k, from)
	}
	f.EnvNames = map[string]string{"openai": "WORK_OPENAI"}
	if k, from, _ := f.Lookup("openai"); k != "sk-work-2222222222222222" || from != "env:WORK_OPENAI" {
		t.Fatalf("api_key_env: %q %q", k, from)
	}
	f.Env = envOf(map[string]string{"WORK_OPENAI": "   "}) // blank env falls through to the file
	if k, from, _ := f.Lookup("openai"); k != "sk-file-0000000000000000" || from != "file" {
		t.Fatalf("fallthrough: %q %q", k, from)
	}
}

func TestMissingKeyHasAHint(t *testing.T) {
	_, _, err := credFile(t).Lookup("openrouter")
	if !errors.Is(err, ErrNoCredentials) || !strings.Contains(err.Error(), "herdr-dots keys set openrouter") || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestPermissiveFileIsRefusedWithoutReadingIt(t *testing.T) {
	f := credFile(t)
	if err := os.WriteFile(f.Path, []byte("[anthropic]\napi_key = \"sk-ant-api03-abcdefghijklmnop\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Lookup("anthropic"); !errors.Is(err, ErrInsecurePerms) {
		t.Fatalf("Lookup err = %v, want ErrInsecurePerms", err)
	}
	if err := f.Set("openai", "sk-xxxxxxxxxxxxxxxxxxxx"); !errors.Is(err, ErrInsecurePerms) {
		t.Fatalf("Set err = %v, want ErrInsecurePerms", err)
	}
	if _, err := f.List(); !errors.Is(err, ErrInsecurePerms) {
		t.Fatalf("List err = %v", err)
	}
}

func TestSymlinkedCredentialsFileIsRefused(t *testing.T) {
	f := credFile(t)
	real := filepath.Join(t.TempDir(), "real.toml")
	if err := os.WriteFile(real, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, f.Path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Lookup("anthropic"); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("err = %v", err)
	}
}

func TestMalformedFileErrorNeverQuotesTheKey(t *testing.T) {
	f := credFile(t)
	if err := os.WriteFile(f.Path, []byte("[anthropic]\napi_key = sk-ant-api03-unquotedsecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.Lookup("anthropic")
	if err == nil || strings.Contains(err.Error(), "unquotedsecret") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetRejectsBadKeysAndUnknownProviders(t *testing.T) {
	f := credFile(t)
	for _, k := range []string{"", "sk-ant- with space", "sk\nnewline", strings.Repeat("a", 513)} {
		if err := f.Set("anthropic", k); !errors.Is(err, ErrBadKey) {
			t.Errorf("Set(%q) err = %v, want ErrBadKey", k, err)
		}
	}
	if err := f.Set("ollama", "x"); err == nil {
		t.Fatal("ollama takes no key")
	}
}

func TestRemoveAndListShowOnlyLast4(t *testing.T) {
	f := credFile(t)
	_ = f.Set("anthropic", "sk-ant-api03-abcdefghWXYZ")
	_ = f.Set("openrouter", "sk-or-v1-0123456789abcd")
	f.Env = envOf(map[string]string{"OPENAI_API_KEY": "sk-proj-zzzzzzzzzzzz9876"})
	got, err := f.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []Status{{"anthropic", "file", "…WXYZ"}, {"openai", "env:OPENAI_API_KEY", "…9876"}, {"openrouter", "file", "…abcd"}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if err := f.Remove("openrouter"); err != nil {
		t.Fatal(err)
	}
	if err := f.Remove("openrouter"); err != nil {
		t.Fatal("removing a missing key is not an error")
	}
	if _, _, err := f.Lookup("openrouter"); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("after remove: %v", err)
	}
	if k, _, _ := f.Lookup("anthropic"); k != "sk-ant-api03-abcdefghWXYZ" {
		t.Fatal("Remove must keep other providers")
	}
}

func TestRedact(t *testing.T) {
	cases := []struct{ in, mustNot, want string }{
		{"invalid x-api-key sk-ant-api03-AbCdEf123456", "AbCdEf123456", "sk-ant-[redacted]"},
		{"key sk-or-v1-0123456789abcdef rejected", "0123456789abcdef", "sk-or-[redacted]"},
		{"Incorrect API key provided: sk-proj-ABCDEFGHIJKLMNOP", "ABCDEFGHIJKLMNOP", "sk-[redacted]"},
		{"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload", "eyJhbGciOiJIUzI1NiJ9", "[redacted]"},
		{`{"api_key": "plainsecretvalue"}`, "plainsecretvalue", "[redacted]"},
		{"GET /v1/models?api_key=querysecret123&x=1", "querysecret123", "[redacted]"},
		{"nothing secret here: model claude-opus-5-5", "", "claude-opus-5-5"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if (c.mustNot != "" && strings.Contains(got, c.mustNot)) || !strings.Contains(got, c.want) {
			t.Errorf("Redact(%q) = %q", c.in, got)
		}
	}
	if got := Redact("echo: my-custom-key-123", "my-custom-key-123"); strings.Contains(got, "my-custom-key-123") {
		t.Errorf("known key not redacted: %q", got)
	}
}
