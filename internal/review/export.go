package review

import "github.com/tomasz-tomczyk/crit/internal/session"

// FindReviewFileByCommentID scans review files for a comment ID.
func FindReviewFileByCommentID(commentID string, excludePath string) (string, error) {
	return findReviewFileByCommentID(commentID, excludePath)
}

// CJContainsCommentID reports whether cj contains the given comment ID.
func CJContainsCommentID(cj *CritJSON, id string) bool {
	return cjContainsCommentID(cj, id)
}

// ErrReviewFileAmbiguousForBranch is returned when multiple review files match a branch.
var ErrReviewFileAmbiguousForBranch = errReviewFileAmbiguousForBranch

// FindReviewFileByBranch locates a review file for the given branch name.
func FindReviewFileByBranch(branch, excludePath string) (string, error) {
	return findReviewFileByBranch(branch, excludePath)
}

// ClearCritJSON removes the review file for the given output directory.
func ClearCritJSON(outputDir string) error {
	return clearCritJSON(outputDir)
}

// LoadSnapshotsFile reads snapshots.json from the given path.
func LoadSnapshotsFile(snapshotsPath string) (SnapshotsFile, error) {
	return loadSnapshotsFile(snapshotsPath)
}

// EnsureReviewFolder migrates legacy flat review files into the v4 folder
// layout. The implementation lives in internal/session so the review.json
// write path (session.SaveCritJSON) can apply it unconditionally.
func EnsureReviewFolder(identity string) error {
	return session.EnsureReviewFolder(identity)
}
