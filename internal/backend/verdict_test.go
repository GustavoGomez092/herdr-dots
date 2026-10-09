package backend

import (
	"errors"
	"testing"
)

// Review Focus 3: tolerate fences/prose and severity synonyms; reject unsafe shapes.
func TestParseVerdict(t *testing.T) {
	ok := []struct {
		in   string
		want Verdict
	}{
		{`{"relevant":false,"severity":"info","message":""}`, Verdict{false, "info", ""}},
		{"```json\n{\"relevant\":true,\"severity\":\"critical\",\"message\":\"XSS\"}\n```", Verdict{true, "critical", "XSS"}},
		{`Here you go: {"relevant": true, "severity": "WARNING", "message": " check  "} thanks`, Verdict{true, "warn", "check"}},
		{`{"relevant":true,"message":"no severity given"}`, Verdict{true, "info", "no severity given"}},
		{`{"relevant":true,"severity":"spicy","message":"x"}`, Verdict{true, "info", "x"}},
	}
	for _, c := range ok {
		got, err := ParseVerdict(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseVerdict(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	bad := []string{
		``,
		`I have nothing to add.`,
		`{"severity":"info","message":"x"}`,       // relevant missing
		`{"relevant":true,"severity":"warn","message":""}`, // relevant but empty
		`{"relevant":"yes","message":"x"}`,        // wrong type
	}
	for _, in := range bad {
		if _, err := ParseVerdict(in); !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseVerdict(%q) err = %v, want ErrMalformed", in, err)
		}
	}
}
