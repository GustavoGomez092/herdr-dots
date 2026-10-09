package secrets_test

import (
	"testing"

	"herdr-dots/internal/memory"
	"herdr-dots/internal/secrets"
)

// The full sample tables live in internal/memory's secret tests (they call
// memory.ScanSecrets, the wrapper). Here: the leaf package finds the same
// things, and the wrapper is exactly Scan.
func TestScanAndTheMemoryWrapperAgree(t *testing.T) {
	for text, kind := range map[string]string{
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE":                                                        "private key",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U": "jwt",
		"password = 'S3cr3tPassw0rd!'":                                                                 "credential assignment",
		"AIzaSyA1234567890abcdefghijklmnopqrstuv":                                                      "google api key",
		"key Zx9Qm2LpT7vR4kW8nB3yH6cJ1fD5gS0a works":                                                   "high-entropy string",
		"Auth tokens live in httpOnly cookies, never localStorage.":                                    "",
		"fixed in commit 9f2c41aa07d3e1b2c4f6a8b0d2e4f6a8b0c2d4e6":                                     "",
	} {
		found, got := secrets.Scan(text)
		if found != (kind != "") || got != kind {
			t.Errorf("Scan(%q) = %v, %q; want %q", text, found, got, kind)
		}
		mf, mk := memory.ScanSecrets(text)
		if mf != found || mk != got {
			t.Errorf("memory.ScanSecrets(%q) = %v, %q; Scan = %v, %q", text, mf, mk, found, got)
		}
	}
}
