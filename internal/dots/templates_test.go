package dots

import (
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
