package paths

import (
	"strings"
	"testing"

	"herdr-dots/internal/platform/platformtest"
)

// L6 (post-rc hardening): a relative plugin config or state directory is
// refused, so files never land under whatever directory a popup runs in.
func TestFromEnvRefusesRelativeDirs(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", platformtest.Abs("sock"))
	for _, c := range []struct{ cfg, state, want string }{
		{"relative/config", platformtest.Abs("s"), "HERDR_PLUGIN_CONFIG_DIR"},
		{platformtest.Abs("c"), "relative/state", "HERDR_PLUGIN_STATE_DIR"},
	} {
		t.Setenv("HERDR_PLUGIN_CONFIG_DIR", c.cfg)
		t.Setenv("HERDR_PLUGIN_STATE_DIR", c.state)
		_, err := FromEnv()
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "not absolute") {
			t.Fatalf("%+v: want %s not absolute, got %v", c, c.want, err)
		}
	}
}
