//go:build !windows

package session

// transientIOErr: POSIX rename/read over an existing file never fails
// transiently, so every error is a real failure.
func transientIOErr(error) bool { return false }
