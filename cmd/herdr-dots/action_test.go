package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToggle(t *testing.T) {
	always := func(string) bool { return true }
	never := func(string) bool { return false }

	m := map[string]string{}
	if c, open := toggle(m, "w1:p1", always); c != "" || !open {
		t.Fatal("no Dots pane yet: open one")
	}
	m = map[string]string{"w1:p1": "w1:p7"}
	if c, open := toggle(m, "w1:p1", always); c != "w1:p7" || open || len(m) != 0 {
		t.Fatal("existing Dots pane for target: close it")
	}
	m = map[string]string{"w1:p1": "w1:p7"}
	if c, open := toggle(m, "w1:p7", always); c != "w1:p7" || open {
		t.Fatal("focused pane is a Dots pane: close it")
	}
	m = map[string]string{"w1:p1": "w1:p7"}
	if c, open := toggle(m, "w1:p1", never); c != "" || !open || len(m) != 0 {
		t.Fatal("stale mapping: forget it and open a new pane")
	}
}

func TestLoadDotsPanesNeverNilNeverErrorsOnBadContent(t *testing.T) {
	dir := t.TempDir()
	m, err := loadDotsPanes(filepath.Join(dir, "missing.json"))
	if err != nil || m == nil {
		t.Fatalf("missing: %v %v", m, err)
	}
	for name, content := range map[string]string{"null": "null", "corrupt": "{not json"} {
		p := filepath.Join(dir, name+".json")
		os.WriteFile(p, []byte(content), 0o600)
		m, err := loadDotsPanes(p)
		if err != nil || m == nil || len(m) != 0 {
			t.Fatalf("%s: %v %v", name, m, err)
		}
		m["x"] = "y" // must not panic
	}
	p := filepath.Join(dir, "ok.json")
	os.WriteFile(p, []byte(`{"a":"b"}`), 0o600)
	if m, _ := loadDotsPanes(p); m["a"] != "b" {
		t.Fatalf("valid: %v", m)
	}
}
