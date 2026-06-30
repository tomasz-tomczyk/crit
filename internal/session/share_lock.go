package session

import (
	"fmt"
	"os"
)

// WithShareLock runs fn while holding an exclusive advisory lock on the review
// identity's share.lock file. Concurrent crit share invocations for the same
// review identity block here so only one POST/upsert+persist cycle runs at a
// time. Callers must re-read share state after acquiring the lock.
func WithShareLock(identity string, fn func() error) error {
	if err := ensureReviewFolder(identity); err != nil {
		return fmt.Errorf("ensuring review folder: %w", err)
	}
	paths := ReviewPathsFor(identity)
	if err := os.MkdirAll(paths.Folder, 0o700); err != nil {
		return fmt.Errorf("creating review folder: %w", err)
	}
	lockPath := paths.ShareLock
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening share lock %q: %w", lockPath, err)
	}
	defer func() {
		_ = funlock(f)
		_ = f.Close()
	}()
	if err := flockExclusive(f); err != nil {
		return fmt.Errorf("acquiring share lock: %w", err)
	}
	return fn()
}
