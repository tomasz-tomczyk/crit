package vcs

// FileDiffUnifiedForTest exposes fileDiffUnified for cross-package tests.
func FileDiffUnifiedForTest(path, baseRef, dir string, ignoreWhitespace bool) ([]DiffHunk, error) {
	return fileDiffUnified(path, baseRef, dir, ignoreWhitespace)
}
