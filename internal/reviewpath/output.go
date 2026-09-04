package reviewpath

import (
	"path/filepath"
)

// absPath is filepath.Abs in production; tests may replace it to exercise errors.
var absPath = filepath.Abs

// ReviewsDir returns the reviews directory under a crit data root:
// {dataRoot}/reviews. dataRoot must be non-empty; the empty/default case is
// daemon.ReviewsDir (~/.crit/reviews).
func ReviewsDir(dataRoot string) (string, error) {
	abs, err := absPath(dataRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, "reviews"), nil
}

// Identity resolves a session key under a crit data root to the review
// identity folder: {dataRoot}/reviews/{key}.
//
// --output / config output means this data root (same role as ~/.crit for
// reviews). Reusing the same root keeps keyed, per-branch behavior; it does
// not pin a single fixed review folder.
func Identity(dataRoot, key string) (string, error) {
	dir, err := ReviewsDir(dataRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, key), nil
}
