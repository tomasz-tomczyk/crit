package reviewpath

import (
	"os"
	"path/filepath"
)

// ReviewsDir returns the reviews directory under a crit data root:
// {dataRoot}/reviews. dataRoot must be non-empty; the empty/default case is
// daemon.ReviewsDir (~/.crit/reviews).
func ReviewsDir(dataRoot string) (string, error) {
	abs, err := filepath.Abs(dataRoot)
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

// LegacyIdentityPath is the pre-data-root layout: {dataRoot}/.crit.
// Used only to detect leftover reviews after the semantics change.
func LegacyIdentityPath(dataRoot string) (string, error) {
	abs, err := filepath.Abs(dataRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, ".crit"), nil
}

// HasLegacyIdentity reports whether dataRoot still contains the old
// fixed-identity folder or flat file from before output meant "data root".
func HasLegacyIdentity(dataRoot string) bool {
	legacy, err := LegacyIdentityPath(dataRoot)
	if err != nil {
		return false
	}
	if st, err := os.Stat(legacy); err == nil && st.IsDir() {
		return true
	}
	if _, err := os.Stat(legacy + ".json"); err == nil {
		return true
	}
	return false
}
