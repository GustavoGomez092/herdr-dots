package dots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P9-R4 and P9-R10 refuse these args for every dot, local ones included, so
// a local dot that used one stops loading after an upgrade. The guide's
// upgrade note must name each one, and each must really be refused.
func TestGuideUpgradeNoteNamesEveryNewlyDeniedArg(t *testing.T) {
	cases := []struct {
		kind, readme string
		args         []string
	}{
		{"claude-cli", "`--system-prompt-file`", []string{"--system-prompt-file", "p.md"}},
		{"codex-cli", "`-c`/`--config` keys other than", []string{"-c", "sandbox_mode=danger-full-access"}},
		{"codex-cli", "`-c`/`--config` keys other than", []string{"--config", "sandbox_mode=danger-full-access"}},
		{"codex-cli", "`--search`", []string{"--search"}},
		{"codex-cli", "`-p`/`--profile`", []string{"-p", "x"}},
		{"codex-cli", "`-p`/`--profile`", []string{"--profile", "x"}},
		{"codex-cli", "`-o`/`--output-last-message`", []string{"-o", "x"}},
		{"codex-cli", "`-o`/`--output-last-message`", []string{"--output-last-message", "x"}},
		{"codex-cli", "`--oss`", []string{"--oss"}},
		{"codex-cli", "`--local-provider`", []string{"--local-provider", "x"}},
		{"codex-cli", "`--enable`/`--disable`", []string{"--enable", "x"}},
		{"codex-cli", "`--enable`/`--disable`", []string{"--disable", "x"}},
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "guide", "profiles-and-sharing.md"))
	if err != nil {
		t.Fatal(err)
	}
	r := strings.Join(strings.Fields(string(data)), " ")
	if !strings.Contains(r, "Upgrading from 0.8") {
		t.Error("guide has no upgrade note")
	}
	for _, c := range cases {
		if len(DeniedArgs(c.kind, c.args)) == 0 {
			t.Errorf("%s %v is not refused", c.kind, c.args)
		}
		if !strings.Contains(r, c.readme) {
			t.Errorf("guide upgrade note doesn't name %s", c.readme)
		}
	}
	for _, k := range codexConfigKeys {
		if !strings.Contains(r, "`"+k+"`") {
			t.Errorf("guide doesn't list the allowed codex -c key %s", k)
		}
	}
}
