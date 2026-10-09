package main

import "testing"

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
