package openaiapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// Final review item 7: a 4xx carries the provider's message and param,
// redacted, sanitized and clipped.
func TestBadRequestShowsMessageAndParam(t *testing.T) {
	long := strings.Repeat("x", 600)
	f := &fake{replies: []string{`STATUS:400:{"error":{"message":"Invalid schema for response_format 'dot_reply' (key ` + testKey + `)\u001b[2J ` + long + `","type":"invalid_request_error","param":"text.format.schema","code":"invalid_json_schema"}}`}}
	_, err := newBackend(t, f).Run(context.Background(), req())
	if err == nil {
		t.Fatal("want an error")
	}
	s := err.Error()
	for _, want := range []string{"rejected the request (400)", "Invalid schema for response_format", "text.format.schema"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
	if strings.Contains(s, "TESTKEY") || strings.ContainsAny(s, "\x1b") || len([]rune(s)) > 420 {
		t.Fatalf("not redacted, sanitized and clipped (%d runes): %q", len([]rune(s)), s)
	}
}

// Final review item 7: status "failed" with an error object is a clear run
// error, not a parse failure, and keeps the usage.
func TestFailedResponseIsAClearRunError(t *testing.T) {
	failed := `{"id":"resp_1","object":"response","created_at":1,"status":"failed","model":"gpt-6-sol","output":[],
	"error":{"code":"server_error","message":"The model crashed for key ` + testKey + `\u0007"},
	"usage":{"input_tokens":50,"input_tokens_details":{"cached_tokens":0},"output_tokens":0,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":50},
	"parallel_tool_calls":true,"tool_choice":"auto","tools":[],"incomplete_details":null,"instructions":null,"metadata":{}}`
	f := &fake{replies: []string{failed}}
	res, err := newBackend(t, f).Run(context.Background(), req())
	if err == nil || errors.Is(err, backend.ErrMalformed) {
		t.Fatalf("err = %v: want a run error, not a parse failure", err)
	}
	s := err.Error()
	if !strings.Contains(s, "server_error") || !strings.Contains(s, "The model crashed") || strings.Contains(s, "TESTKEY") || strings.ContainsAny(s, "\x07") {
		t.Fatalf("err = %q", s)
	}
	if res.InputTokens != 50 {
		t.Fatalf("usage lost: %+v", res)
	}
}
