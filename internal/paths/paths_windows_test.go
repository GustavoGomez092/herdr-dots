//go:build windows

package paths

import (
	"strings"
	"testing"
)

func TestEngineEndpointIsAPerSessionPipe(t *testing.T) {
	sock := `C:\Users\José Núñez\AppData\Roaming\herdr\herdr.sock`
	p := New(`C:\c`, `C:\s`, sock)
	if got := p.EngineEndpoint(); got != `\\.\pipe\herdr-dots-`+SessionKey(sock) || strings.ContainsAny(got[len(`\\.\pipe\`):], `\/`) {
		t.Fatalf("endpoint %q", got)
	}
}
