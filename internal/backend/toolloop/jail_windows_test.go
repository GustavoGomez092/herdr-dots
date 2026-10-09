//go:build windows

package toolloop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamsDevicesAndTrailingDotsAreDenied(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("API_KEY=x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".env::$DATA", ".env:secret", "NUL", "src/com1.txt", ".env.", ".env "} {
		if _, _, err := ResolveInRoot(root, p, DefaultDeny); !errors.Is(err, ErrDenied) {
			t.Errorf("%q: err = %v, want ErrDenied", p, err)
		}
	}
}
