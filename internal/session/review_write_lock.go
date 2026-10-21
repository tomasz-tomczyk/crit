package session

import (
	"errors"
	"fmt"
	"os"
)

// WithReviewWriteLock serializes read-modify-write transactions on review.json.
// Writers must read their disk state after acquiring it. Keep it separate from
// share.lock, which spans network calls and can contain review-file writes.
func WithReviewWriteLock(identity string, fn func() error) error {
	f, err := lockReview(identity)
	if err != nil {
		return err
	}
	defer func() {
		_ = funlock(f)
		_ = f.Close()
	}()
	return fn()
}

// lockReviewAttempts bounds retries when the lock file keeps being replaced.
const lockReviewAttempts = 10

// lockReview opens and locks the review's lock file. When ClearReviewFolder
// removed the folder while this caller waited, the lock it got is on a file
// no longer at the path, and a later caller could lock a new file at the same
// time. It then starts again with whatever file is at the path now.
func lockReview(identity string) (*os.File, error) {
	paths := ReviewPathsFor(identity)
	for attempt := 0; attempt < lockReviewAttempts; attempt++ {
		if err := ensureReviewFolder(identity); err != nil {
			return nil, fmt.Errorf("ensuring review folder: %w", err)
		}
		if err := os.MkdirAll(paths.Folder, 0o700); err != nil {
			return nil, fmt.Errorf("creating review folder: %w", err)
		}
		f, err := os.OpenFile(paths.ReviewLock, os.O_CREATE|os.O_RDWR, 0o644)
		if os.IsNotExist(err) {
			continue // the folder was removed after MkdirAll; create it again
		}
		if err != nil {
			return nil, fmt.Errorf("opening review lock: %w", err)
		}
		if err := flockExclusive(f); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("locking review: %w", err)
		}
		if lockFileCurrent(f, paths.ReviewLock) {
			return f, nil
		}
		_ = funlock(f)
		_ = f.Close()
	}
	return nil, errors.New("locking review: lock file kept being replaced")
}

// lockFileCurrent reports whether f is still the file at path.
func lockFileCurrent(f *os.File, path string) bool {
	held, err := f.Stat()
	if err != nil {
		return false
	}
	onDisk, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(held, onDisk)
}
