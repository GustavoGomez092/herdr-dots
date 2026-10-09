package platform

import (
	"fmt"
	"io"
	"sync"
)

// noBreakawayNoted makes the "stays in the caller's job" note once per
// process.
var noBreakawayNoted sync.Once

// startDetachedRetry is StartDetached's Windows retry, kept OS-neutral so
// every OS can test it. start(true) asks to break away from the caller's
// job; when that is denied (a job that forbids breakaway), it notes once on
// w (the engine log) that the process stays in the job, then runs
// start(false).
func startDetachedRetry(start func(breakaway bool) error, denied func(error) bool, w io.Writer, once *sync.Once) error {
	err := start(true)
	if err == nil || !denied(err) {
		return err
	}
	once.Do(func() {
		if w != nil {
			fmt.Fprintln(w, "herdr-dots: the caller's job forbids breakaway; the engine starts inside it and ends if that job is closed with kill-on-close")
		}
	})
	return start(false)
}
