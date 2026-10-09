package platform

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

var errDenied = errors.New("access denied")

func isDenied(err error) bool { return errors.Is(err, errDenied) }

// StartDetached's Windows retry, run on every OS: a job that forbids
// breakaway makes the first start fail with access denied; the start is
// retried inside the job and that is noted once, because an engine in a
// kill-on-close job dies with it.
func TestStartDetachedRetryNotesTheJobOnce(t *testing.T) {
	var once sync.Once
	var log bytes.Buffer
	for i := 0; i < 2; i++ {
		var tries []bool
		err := startDetachedRetry(func(breakaway bool) error {
			tries = append(tries, breakaway)
			if breakaway {
				return errDenied
			}
			return nil
		}, isDenied, &log, &once)
		if err != nil || len(tries) != 2 || !tries[0] || tries[1] {
			t.Fatalf("run %d: err %v tries %v", i, err, tries)
		}
	}
	if n := strings.Count(log.String(), "kill-on-close"); n != 1 {
		t.Fatalf("noted %d times: %q", n, log.String())
	}
}

func TestStartDetachedRetryOnlyOnAccessDenied(t *testing.T) {
	var once sync.Once
	var log bytes.Buffer
	boom := errors.New("not found")
	calls := 0
	err := startDetachedRetry(func(bool) error { calls++; return boom }, isDenied, &log, &once)
	if !errors.Is(err, boom) || calls != 1 || log.Len() != 0 {
		t.Fatalf("err %v calls %d log %q", err, calls, log.String())
	}
	calls = 0
	if err := startDetachedRetry(func(bool) error { calls++; return nil }, isDenied, &log, &once); err != nil || calls != 1 || log.Len() != 0 {
		t.Fatalf("breakaway allowed: err %v calls %d log %q", err, calls, log.String())
	}
	// A nil writer is fine.
	if err := startDetachedRetry(func(b bool) error {
		if b {
			return errDenied
		}
		return nil
	}, isDenied, nil, &sync.Once{}); err != nil {
		t.Fatal(err)
	}
}
