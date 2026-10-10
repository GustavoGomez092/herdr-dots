package delivery

import "testing"

func TestTruncateRunesNeverPanics(t *testing.T) {
	for _, c := range []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 0, ""}, {"hello", -3, ""}, {"", 0, ""}, {"hello", 1, "…"}, {"héllo", 3, "hé…"}, {"hi", 5, "hi"},
	} {
		if got := TruncateRunes(c.s, c.n); got != c.want {
			t.Errorf("TruncateRunes(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}
