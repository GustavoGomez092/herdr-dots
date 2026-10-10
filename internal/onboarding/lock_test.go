package onboarding

import "herdr-dots/internal/lockfile"

func lockAt(path string) (*lockfile.Lock, error) { return lockfile.TryLock(path) }
