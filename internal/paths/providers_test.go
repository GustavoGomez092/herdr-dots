package paths

import (
	"path/filepath"
	"testing"
)

func TestProviderFilesLiveInTheConfigDir(t *testing.T) {
	p := New("/cfg", "/state", "/tmp/herdr.sock")
	if p.CredentialsFile() != filepath.Join("/cfg", "credentials.toml") || p.PricesFile() != filepath.Join("/cfg", "prices.toml") {
		t.Fatalf("%s %s", p.CredentialsFile(), p.PricesFile())
	}
}
