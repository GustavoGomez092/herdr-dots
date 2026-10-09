package dots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNoDotCanWriteByDefault(t *testing.T) {
	if c := New("Security").Capabilities; c != DefaultCapabilities() || c.Write || c.WriteTimeout() != 10*time.Minute || c.MaxPatchBytes != 200000 || c.MaxFiles != 50 {
		t.Fatalf("defaults %+v", c)
	}
	for _, d := range Templates() {
		if d.Capabilities != DefaultCapabilities() {
			t.Errorf("template %s capabilities %+v", d.Slug, d.Capabilities)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "security.toml"), []byte(v1Dot), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := Load(dir, "security")
	if err != nil || d.Capabilities != DefaultCapabilities() {
		t.Fatalf("a dot file without [capabilities]: %+v %v", d.Capabilities, err)
	}
}

func TestCapabilitiesTableLoadsAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "security.toml"), []byte(v1Dot+"\n[capabilities]\nwrite = true\nmax_files = 12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := Load(dir, "security")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Capabilities.Write || d.Capabilities.MaxFiles != 12 || d.Capabilities.WriteTimeoutSeconds != 600 || d.Capabilities.MaxPatchBytes != 200000 {
		t.Fatalf("capabilities %+v", d.Capabilities)
	}
	if err := Save(dir, d, false); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "security")
	if err != nil || got.Capabilities != d.Capabilities {
		t.Fatalf("round trip %+v %v", got.Capabilities, err)
	}
}

func TestCodexDotsCannotBeWriteCapable(t *testing.T) {
	d := New("Fixer")
	d.Purpose = "Fix things."
	d.Capabilities.Write = true
	for _, kind := range WriteKinds {
		d.Backend.Kind = kind
		if err := d.Capabilities.validate(kind); len(err) != 0 {
			t.Errorf("%s: %v", kind, err)
		}
	}
	d.Backend.Kind = "codex-cli"
	err := d.Validate()
	if err == nil || !strings.Contains(err.Error(), "codex-cli always has a shell") {
		t.Fatalf("codex write: %v", err)
	}
	d.Capabilities.Write = false
	if err := d.Validate(); err != nil {
		t.Fatalf("a read-only codex dot stays valid: %v", err)
	}
	if CanWrite("codex-cli") || !CanWrite("claude-cli") {
		t.Fatal("CanWrite")
	}
}

func TestCapabilityLimitsAreRangeChecked(t *testing.T) {
	d := New("Fixer")
	d.Purpose = "Fix things."
	d.Capabilities = CapabilitySpec{WriteTimeoutSeconds: 5, MaxPatchBytes: 10, MaxFiles: 0}
	err := d.Validate()
	for _, want := range []string{"write_timeout_seconds must be 60-3600", "max_patch_bytes must be 1000-2000000", "max_files must be 1-500"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}
