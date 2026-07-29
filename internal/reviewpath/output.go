package reviewpath

import (
	"fmt"
	"os"
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

// IdentityUnderDataRoot resolves a session key under a crit data root,
// preferring a pre-existing legacy {dataRoot}/.crit review when one is there.
//
// Before --output meant a data root it named one fixed review folder, so roots
// written by older versions already hold review.json, the share URL and the
// delete token at {dataRoot}/.crit. Keying underneath such a root would strand
// that review: `crit share` would publish a duplicate and `crit fetch` /
// `crit unpublish` would report no review file. The legacy folder therefore
// keeps winning, with a warning, until the user moves or removes it.
func IdentityUnderDataRoot(dataRoot, key string) (string, error) {
	if HasLegacyIdentity(dataRoot) {
		legacy, err := LegacyIdentityPath(dataRoot)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(os.Stderr, "crit: warning: using the legacy .crit review in %s, from when --output named a single review folder; output is now a data root, so move or remove it to get keyed, per-branch reviews under %s\n",
			dataRoot, filepath.Join(dataRoot, "reviews"))
		return legacy, nil
	}
	return Identity(dataRoot, key)
}

// LegacyIdentityPath is the pre-data-root layout: {dataRoot}/.crit.
func LegacyIdentityPath(dataRoot string) (string, error) {
	abs, err := absPath(dataRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, ".crit"), nil
}

// HasLegacyIdentity reports whether dataRoot contains the old fixed-identity
// folder or flat file from before output meant "data root".
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
