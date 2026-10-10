package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// B20 (post-rc hardening, test gap): a second 400 after the prompt-only
// fallback fails the run; it is not retried again.
func TestSecondRejectionAfterTheFallbackFailsTheRun(t *testing.T) {
	reject := func(w http.ResponseWriter) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"Provider does not support response_format json_schema"}}`)
	}
	f := &fake{replies: []func(http.ResponseWriter){reject, reject}}
	_, err := newBackend(t, f, nil).Run(context.Background(), req())
	if err == nil || len(f.bodies) != 2 {
		t.Fatalf("err = %v after %d requests, want an error after 2", err, len(f.bodies))
	}
	if _, has := f.bodies[1]["response_format"]; has {
		t.Fatal("the second request is the prompt-only one")
	}
}

// B18 (post-rc hardening): a cancelled run (pane closed, shutdown) is
// reported as cancelled, not as a timeout.
func TestCancelledRunIsNotReportedAsATimeout(t *testing.T) {
	err := mapErr(context.Canceled, "m")
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v", err)
	}
	if err := mapErr(context.DeadlineExceeded, "m"); !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("a deadline is still a timeout: %v", err)
	}
}
