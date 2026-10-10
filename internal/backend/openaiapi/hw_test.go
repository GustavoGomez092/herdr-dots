package openaiapi

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// B18 (post-rc hardening): a cancelled run (pane closed, shutdown) is
// reported as cancelled, not as a timeout.
func TestCancelledRunIsNotReportedAsATimeout(t *testing.T) {
	err := mapErr(context.Canceled, "", "m")
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v", err)
	}
	if err := mapErr(context.DeadlineExceeded, "", "m"); !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("a deadline is still a timeout: %v", err)
	}
}
