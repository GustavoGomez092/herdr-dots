package dots

import "testing"

func TestTemplatesAreAllValid(t *testing.T) {
	ts := Templates()
	if len(ts) != 7 {
		t.Fatalf("want 7 templates, got %d", len(ts))
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
