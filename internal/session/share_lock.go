package session

// WithShareLock runs fn while holding an exclusive advisory lock on the review
// identity's share.lock file. Concurrent crit share invocations for the same
// review identity block here so only one POST/upsert+persist cycle runs at a
// time. Callers must re-read share state after acquiring the lock. Like the
// review lock, it checks that the locked file is still the one at the path,
// since ClearReviewFolder can delete share.lock while it is held.
func WithShareLock(identity string, fn func() error) error {
	f, err := lockIdentityFile(identity, ReviewPathsFor(identity).ShareLock, "share lock")
	if err != nil {
		return err
	}
	defer func() {
		_ = funlock(f)
		_ = f.Close()
	}()
	return fn()
}
