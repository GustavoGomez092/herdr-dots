package lockfile

import (
	"errors"
	"time"
)

// pollInterval is how often Acquire retries a held lock.
const pollInterval = 25 * time.Millisecond

// Acquire polls TryLock until it gets the lock or timeout passes, then returns
// ErrLocked. A timeout of 0 tries exactly once.
func Acquire(path string, timeout time.Duration) (*Lock, error) {
	deadline := time.Now().Add(timeout)
	for {
		l, err := TryLock(path)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, ErrLocked
		}
		time.Sleep(min(pollInterval, max(time.Millisecond, time.Until(deadline))))
	}
}
