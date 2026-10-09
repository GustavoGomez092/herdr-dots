package memory

import "testing"

func TestScanSecretsFindsCredentials(t *testing.T) {
	cases := []struct{ text, kind string }{
		{"the staging key is AKIAIOSFODNN7EXAMPLE", "aws access key"},
		{"use ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789 for CI", "github token"},
		{"github_pat_11ABCDEFG0123456789_abcdefghijKLMNOP", "github token"},
		{"glpat-xYz123ABCdef456GHIjkl", "gitlab token"},
		{"bot token xoxb-1234567890-abcdefghij", "slack token"},
		{"stripe sk_live_4eC39HqLyjWDarjtT1zdp7dc", "stripe key"},
		{"OPENAI_API_KEY is sk-proj-abcDEF123ghiJKL456mnoPQR789", "openai key"},
		{"sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345", "anthropic key"},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIE", "private key"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----", "private key"},
		{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "jwt"},
		{"password = 'S3cr3tPassw0rd!'", "credential assignment"},
		{`api_key: "AbCdEf123456GhIj"`, "credential assignment"},
		{"key Zx9Qm2LpT7vR4kW8nB3yH6cJ1fD5gS0a works", "high-entropy string"},
	}
	for _, c := range cases {
		found, kind := ScanSecrets(c.text)
		if !found || kind != c.kind {
			t.Errorf("ScanSecrets(%q) = %v, %q; want true, %q", c.text, found, kind, c.kind)
		}
	}
}

func TestScanSecretsIgnoresLookAlikes(t *testing.T) {
	for _, text := range []string{
		"fixed in commit 9f2c41aa07d3e1b2c4f6a8b0d2e4f6a8b0c2d4e6",
		"session 81ef4218-b6bb-4f49-aba5-9156edd7320e",
		"see internal/engine/memory_hooks_handler_registry.go",
		"handleAuthenticationCallbackForProviderV2 is too long",
		"Auth tokens live in httpOnly cookies, never localStorage.",
		"max_tokens=4096 keeps replies short",
		"token=<redacted>",
		"password: see the team vault",
		"sk-learn and scikit are fine",
		"secret = ${STRIPE_KEY}",
		"api_key: YOUR_API_KEY_HERE",
		"Auth token: localStorage is unsafe",
	} {
		if found, kind := ScanSecrets(text); found {
			t.Errorf("false positive %q (%s)", text, kind)
		}
	}
}
