package anthropicapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// B25 (post-rc hardening): when the web-use budget stops a paused research
// turn, the error says so instead of counting pauses.
func TestWebBudgetStopSaysTheBudgetStoppedIt(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){
		searchMsg("pause_turn", pausedContent, 5),
		searchMsg("end_turn", text(verdictPass), 0), // never requested
	}}
	b, _ := newBackend(t, f)
	_, err := b.Run(context.Background(), webReq())
	if err == nil || !strings.Contains(err.Error(), "5 web uses") || !strings.Contains(err.Error(), "without finishing") {
		t.Fatalf("err = %v", err)
	}
}

// B18 (post-rc hardening): a cancelled run (pane closed, shutdown) is
// reported as cancelled, not as a timeout.
func TestCancelledRunIsNotReportedAsATimeout(t *testing.T) {
	err := mapErr(context.Canceled, nil, "m")
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v", err)
	}
	if err := mapErr(context.DeadlineExceeded, nil, "m"); !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("a deadline is still a timeout: %v", err)
	}
}
