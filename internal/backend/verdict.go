package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"herdr-dots/internal/textutil"
)

var ErrMalformed = errors.New("malformed verdict")

// ParseVerdict extracts the JSON object from model output (jsonSpan). It
// tolerates code fences and surrounding prose, even prose with braces,
// normalizes severity, and rejects a missing "relevant" or a relevant
// verdict with no message.
func ParseVerdict(text string) (Verdict, error) {
	span, ok := jsonSpan(text, "relevant")
	if !ok {
		return Verdict{}, fmt.Errorf("%w: no JSON object in %q", ErrMalformed, clipErr(text))
	}
	var raw struct {
		Relevant *bool  `json:"relevant"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal([]byte(span), &raw); err != nil {
		return Verdict{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if raw.Relevant == nil {
		return Verdict{}, fmt.Errorf("%w: missing \"relevant\"", ErrMalformed)
	}
	v := Verdict{Relevant: *raw.Relevant, Message: strings.TrimSpace(textutil.SanitizeModelText(raw.Message))}
	switch strings.ToLower(strings.TrimSpace(raw.Severity)) {
	case "critical":
		v.Severity = "critical"
	case "warn", "warning":
		v.Severity = "warn"
	default:
		v.Severity = "info"
	}
	if v.Relevant && v.Message == "" {
		return Verdict{}, fmt.Errorf("%w: relevant verdict with empty message", ErrMalformed)
	}
	return v, nil
}

func clipErr(s string) string {
	r := []rune(s)
	if len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return s
}
