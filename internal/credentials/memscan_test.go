package credentials_test

import (
	"strings"
	"testing"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/memory"
)

// Every provider key shape this package stores is also a secret to Phase 2's
// memory scanner, so a model that echoes a key into a remember op (or a user
// pasting one into memory) is refused, and Redact scrubs the same shapes from
// logs and the feed (reconciled).
func TestEveryProviderKeyShapeIsAMemorySecret(t *testing.T) {
	for _, key := range []string{
		"sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz012345",
		"sk-or-v1-0123456789abcdef0123456789abcdef0123456789abcdef",
		"sk-proj-ABCDEFGHIJKLMNOPQRSTUVWXYZ012345",
	} {
		if found, _ := memory.ScanSecrets("remember: the key is " + key); !found {
			t.Errorf("memory.ScanSecrets misses %s…", key[:8])
		}
		if got := credentials.Redact("401: invalid key " + key); strings.Contains(got, key[len(key)-12:]) {
			t.Errorf("Redact leaves %q", got)
		}
	}
}
