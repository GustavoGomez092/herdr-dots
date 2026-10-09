package memory

import (
	"errors"
	"testing"
)

func TestParseRefRoundTrip(t *testing.T) {
	cases := []struct {
		in, dot string
		want    Ref
	}{
		{"p:m3", "", Ref{Scope: ScopeProject, ID: "m3"}},
		{"p:m3", "security", Ref{Scope: ScopeProject, ID: "m3"}},
		{"d:m17", "security", Ref{Scope: ScopeDot, Dot: "security", ID: "m17"}},
		{" g:m2 ", "copy", Ref{Scope: ScopeGlobal, Dot: "copy", ID: "m2"}},
	}
	for _, c := range cases {
		got, err := ParseRef(c.in, c.dot)
		if err != nil || got != c.want {
			t.Errorf("ParseRef(%q, %q) = %+v, %v; want %+v", c.in, c.dot, got, err, c.want)
		}
		if got.String() != c.want.String() {
			t.Errorf("String() = %q", got.String())
		}
	}
	for _, bad := range []struct{ in, dot string }{{"m3", ""}, {"x:m3", ""}, {"d:m3", ""}, {"g:m2", ""}, {"p:3", ""}, {"p:", ""}, {"p:mx", ""}} {
		if _, err := ParseRef(bad.in, bad.dot); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseRef(%q, %q) err = %v, want ErrInvalid", bad.in, bad.dot, err)
		}
	}
}

func TestEntryRefIgnoresAuthorForProjectScope(t *testing.T) {
	e := Entry{ID: "m31", Scope: ScopeProject, Dot: "frontend"}
	if e.Ref() != (Ref{Scope: ScopeProject, ID: "m31"}) || e.Ref().String() != "p:m31" {
		t.Fatalf("ref %+v", e.Ref())
	}
	d := Entry{ID: "m9", Scope: ScopeDot, Dot: "security"}
	if d.Ref() != (Ref{Scope: ScopeDot, Dot: "security", ID: "m9"}) {
		t.Fatalf("ref %+v", d.Ref())
	}
}

func TestLimitsFromConfig(t *testing.T) {
	l := LimitsFromConfig(200, 90, true)
	if l.MaxDot != 200 || l.MaxProject != 300 || l.MaxGlobal != 100 || l.RetentionDays != 90 || !l.AutoApproveProject {
		t.Fatalf("%+v", l)
	}
	if d := LimitsFromConfig(0, 0, false); d != DefaultLimits() {
		t.Fatalf("zero config must keep defaults: %+v", d)
	}
}
