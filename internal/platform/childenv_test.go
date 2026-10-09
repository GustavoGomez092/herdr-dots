package platform

import (
	"slices"
	"testing"
)

// The Windows ChildEnv logic, run on every OS: the variable is added once,
// replaces any spelling of an existing one, and the caller's slice is left
// untouched.
func TestNoCwdExeSearchEnv(t *testing.T) {
	in := []string{"PATH=/bin", "nodefaultcurrentdirectoryinexepath=0", "A=b", "NoDefaultCurrentDirectoryInExePath="}
	orig := slices.Clone(in)
	got := noCwdExeSearchEnv(in)
	want := []string{"PATH=/bin", "A=b", NoCwdExeSearch + "=1"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !slices.Equal(in, orig) {
		t.Fatalf("input changed to %q", in)
	}
	if got := noCwdExeSearchEnv(nil); !slices.Equal(got, []string{NoCwdExeSearch + "=1"}) {
		t.Fatalf("empty env: %q", got)
	}
	// A variable whose name merely starts the same way is kept.
	if got := noCwdExeSearchEnv([]string{"NoDefaultCurrentDirectoryInExePathX=1"}); len(got) != 2 {
		t.Fatalf("prefix-named variable dropped: %q", got)
	}
}
