//go:build integration

package review

import "testing"

// WalkReviewIdentities iterates review identity folders for integration tests.
func WalkReviewIdentities(visit func(identity string, data []byte) error) error {
	return walkReviewIdentities(visit)
}

// WriteFolderReviewFixture writes a folder-form review under ReviewsDir.
func WriteFolderReviewFixture(t *testing.T, name, branch string) string {
	return writeFolderReviewFixture(t, name, branch)
}
