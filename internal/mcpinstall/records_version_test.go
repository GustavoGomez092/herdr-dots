package mcpinstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/fsutil"
)

// mcp-install.json a newer herdr-dots wrote (possibly after this process
// started) is refused by LoadRecords and never overwritten by SaveRecords.
func TestNewerInstallRecordsAreRefusedAndNeverRewritten(t *testing.T) {
	p := RecordPath(t.TempDir())
	future := `{"version":2,"installs":[{"client":"claude","scope":"user","command":["x"],"config_path":"/c","at":"2026-10-08T12:00:00Z","pinned":true}]}`
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecords(p); !errors.Is(err, ErrRecordsVersion) || !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("LoadRecords on a newer file: %v", err)
	}
	err := SaveRecords(p, []Record{{Client: ClientClaude, Scope: "user", Command: []string{"y"}, At: time.Now()}})
	if !errors.Is(err, ErrRecordsVersion) {
		t.Fatalf("SaveRecords over a newer file: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != future {
		t.Fatalf("newer records changed: %s", b)
	}
	if got := StatusLine(filepath.Dir(p)); got == "MCP: not installed (run herdr-dots setup-mcp)" {
		t.Fatalf("a newer record must not read as nothing installed: %q", got)
	}
}
