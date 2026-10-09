package style

import (
	"strings"
	"testing"
)

func TestWrapKeepsParagraphsAndNeverCuts(t *testing.T) {
	in := "first line \x1b[31mred\x1b[0m\n\nthird " + strings.Repeat("x", 25)
	got := Wrap(in, 10)
	want := []string{"first line", "[31mred[0m", "", "third", "xxxxxxxxxx", "xxxxxxxxxx", "xxxxx"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	if Clean(" a\n\tb\x07 ") != "a b" {
		t.Fatalf("clean %q", Clean(" a\n\tb\x07 "))
	}
}
