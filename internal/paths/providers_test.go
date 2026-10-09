package paths

import "testing"

func TestProviderFilesLiveInTheConfigDir(t *testing.T) {
	p := New("/cfg", "/state", "/tmp/herdr.sock")
	if p.CredentialsFile() != "/cfg/credentials.toml" || p.PricesFile() != "/cfg/prices.toml" {
		t.Fatalf("%s %s", p.CredentialsFile(), p.PricesFile())
	}
}
