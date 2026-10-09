package credentials

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactDoesNotMangleOrdinaryWords(t *testing.T) {
	for _, in := range []string{"task-management-system-design", "disk-usage-statistics", "ask-before-deleting-files", "risk-assessment-framework", "bearer authentication failed"} {
		if got := Redact(in); got != in {
			t.Errorf("Redact(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestRedactMasksWholeAuthorizationValues(t *testing.T) {
	cases := []struct{ in, secret string }{
		{"Authorization: Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{"Authorization: token ghp_abcdefghijklmnop", "ghp_abcdefghijklmnop"},
		{"Authorization: Bearer abcdefghijklmnopqrstuvwx", "abcdefghijklmnopqrstuvwx"},
		{"authorization=Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{"key sk-ant-api03-AbCdEf123456", "AbCdEf123456"},
		{"in task-sk-ant-api03-AbCdEf123456 x", "AbCdEf123456"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if strings.Contains(got, c.secret) || !strings.Contains(got, Redacted) {
			t.Errorf("Redact(%q) = %q", c.in, got)
		}
	}
}

func TestSymlinkRefusedEvenWhenTargetIsOwnerOnly(t *testing.T) {
	f := credFile(t)
	real := filepath.Join(t.TempDir(), "real.toml")
	if err := os.WriteFile(real, []byte("[anthropic]\napi_key = \"sk-ant-api03-abcdefghijklmnop\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, real, f.Path)
	k, _, err := f.Lookup("anthropic")
	if err == nil || k != "" || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("key %q err %v", k, err)
	}
	if err := f.Set("openai", "sk-xxxxxxxxxxxxxxxxxxxx"); err == nil {
		t.Fatal("Set through a symlink must fail")
	}
}

func TestEntryNeverFormatsItsKey(t *testing.T) {
	m := map[string]entry{"anthropic": {APIKey: "sk-ant-api03-supersecretvalue"}}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if got := fmt.Sprintf(verb, m); strings.Contains(got, "supersecretvalue") {
			t.Errorf("%s leaks: %s", verb, got)
		}
	}
}
