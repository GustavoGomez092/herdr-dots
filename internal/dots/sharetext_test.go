package dots

import (
	"errors"
	"strings"
	"testing"
)

func TestUnsafeTextRefusesControlBidiAndHiddenCharacters(t *testing.T) {
	for _, s := range []string{"plain", "Prefer zod — at API boundaries.", "src/api/**"} {
		if err := UnsafeText(s, false); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	if err := UnsafeText("line one\nline two\twith a tab", true); err != nil {
		t.Fatalf("multiline text: %v", err)
	}
	for name, s := range map[string]string{
		"newline (single line)": "a\nb",
		"OSC 52":                "\x1b]52;c;ZXZpbA==\x07",
		"CSI":                   "\x1b[2J",
		"DEL":                   "a\x7fb",
		"C1":                    "a\u009bb",
		"RLO":                   "a‮b",
		"isolate":               "a⁦b",
		"tag":                   "a\U000E0041b",
		"zero width space":      "a​b",
		"ZWJ":                   "a‍b",
		"line separator":        "a b",
	} {
		if err := UnsafeText(s, false); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := UnsafeText("a\x1bb", true); err == nil {
		t.Fatal("multiline allowed ESC")
	}
}

func TestEncodeSecretErrorIsTyped(t *testing.T) {
	d := New("Leaky")
	d.Purpose = "use sk-ant-" + strings.Repeat("a1B2", 8) + " for calls"
	_, err := Encode(d)
	var se *SecretError
	if !errors.As(err, &se) || se.Field != "purpose" {
		t.Fatalf("err %v", err)
	}
	if !strings.HasPrefix(err.Error(), "purpose looks like it holds a secret") || strings.Contains(err.Error(), "a1B2") {
		t.Fatalf("message %q", err)
	}
}
