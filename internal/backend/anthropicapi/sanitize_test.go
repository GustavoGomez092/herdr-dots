package anthropicapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// Final review item 11: model text in engine log lines is sanitized.
func TestFallbackLogLineSanitizesTheServedModel(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		fmt.Fprintf(w, `{"id":"m","type":"message","role":"assistant","model":"claude-evil\u001b]0;pwned\u0007","content":%s,"stop_reason":"end_turn","stop_details":null,"usage":{"input_tokens":1,"output_tokens":1}}`, text(verdictPass))
	}}}
	b, logs := newBackend(t, f)
	if _, err := b.Run(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if len(*logs) == 0 {
		t.Fatal("no fallback log line")
	}
	for _, l := range *logs {
		if strings.ContainsAny(l, "\x1b\x07") {
			t.Fatalf("control characters reached the log: %q", l)
		}
	}
}

func TestRefusalCategoryIsSanitized(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cy\u001b[2Jber"},"usage":{"input_tokens":1,"output_tokens":0}}`)
	}}}
	b, _ := newBackend(t, f)
	_, err := b.Run(context.Background(), req())
	if !errors.Is(err, backend.ErrRefused) || strings.ContainsAny(err.Error(), "\x1b") {
		t.Fatalf("err = %q", err)
	}
}
