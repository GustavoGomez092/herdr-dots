package dots

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func TestTemplatesAreAllValid(t *testing.T) {
	ts := Templates()
	if len(ts) != 14 {
		t.Fatalf("want 14 templates, got %d", len(ts))
	}
	for _, d := range ts {
		if err := d.Validate(); err != nil {
			t.Errorf("template %s invalid: %v", d.Slug, err)
		}
	}
}

func TestFromTemplate(t *testing.T) {
	d, err := FromTemplate("security")
	if err != nil || d.Name != "Security" || d.Glyph != "◆" {
		t.Fatalf("security template: %+v %v", d, err)
	}
	if _, err := FromTemplate("nope"); err == nil {
		t.Fatal("unknown template must error")
	}
}

// P9-R16: templates are your own dots, so MCP asks from your agent are
// allowed; every other risky capability is off. The exact defaults are
// pinned here and the guide says the same, so the two can't drift.
func TestTemplateCapabilitiesMatchTheGuide(t *testing.T) {
	want := CapabilitySpec{Write: false, WriteTimeoutSeconds: 600, MaxPatchBytes: 200000, MaxFiles: 50, MCP: true}
	for _, d := range Templates() {
		if d.Capabilities != want {
			t.Errorf("%s: capabilities %+v, want %+v", d.Slug, d.Capabilities, want)
		}
		if d.Research.Enabled || d.Research.Web || d.Schedule.Cron != "" || d.Trigger.OnCommit {
			t.Errorf("%s: research %+v schedule %q on_commit %v", d.Slug, d.Research, d.Schedule.Cron, d.Trigger.OnCommit)
		}
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "docs", "guide", "profiles-and-sharing.md"))
	if err != nil {
		t.Fatal(err)
	}
	r := strings.Join(strings.Fields(string(readme)), " ") // lines wrap anywhere
	for _, s := range []string{
		fmt.Sprintf("There are %d built-in templates", len(Templates())),
		"write, research, web, schedules and commit reviews are off; MCP asks from your agent are allowed (your own dots)",
	} {
		if !strings.Contains(r, s) {
			t.Errorf("guide doesn't say %q", s)
		}
	}
	if strings.Contains(r, "all with every risky feature off") {
		t.Error("guide still says templates have every risky feature off (MCP is on)")
	}
}

func TestTemplatesShipRiskyFeaturesOff(t *testing.T) {
	for _, d := range Templates() {
		if d.Capabilities.Write {
			t.Errorf("%s: write capability on", d.Slug)
		}
		if d.Research.Enabled || d.Research.Web {
			t.Errorf("%s: research on: %+v", d.Slug, d.Research)
		}
		if d.Schedule.Cron != "" {
			t.Errorf("%s: schedule on: %q", d.Slug, d.Schedule.Cron)
		}
		if d.Trigger.OnCommit {
			t.Errorf("%s: on_commit on", d.Slug)
		}
		for _, r := range d.Purpose {
			if r != '\n' && unicode.IsControl(r) {
				t.Errorf("%s: control character %U in purpose", d.Slug, r)
			}
		}
	}
}
