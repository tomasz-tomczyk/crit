//go:build windows

package session

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// renameAtomic replaces dst with src. On Windows os.Rename uses MoveFileEx
// with MOVEFILE_REPLACE_EXISTING, which fails with ERROR_ACCESS_DENIED or
// ERROR_SHARING_VIOLATION if another process has dst open without
// FILE_SHARE_DELETE. Most short-lived readers (json.Unmarshal after
// os.ReadFile) hold the handle for only microseconds, so a brief retry loop
// recovers reliably. Git for Windows, rclone, and Go's own module cache use
// the same recipe.
func renameAtomic(src, dst string) error {
	const maxAttempts = 10
	delay := 1 * time.Millisecond
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = os.Rename(src, dst)
		if err == nil {
			return nil
		}
		if !isWindowsTransientIOErr(err) {
			return err
		}
		time.Sleep(delay)
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
	return err
}

// readFileShared reads path with the same retry recipe as renameAtomic.
// While renameAtomic protects writers from short-lived readers, the inverse
// race exists too: os.ReadFile fails with ERROR_SHARING_VIOLATION /
// ERROR_LOCK_VIOLATION when a writer is mid-rename over the destination.
// MoveFileEx with MOVEFILE_REPLACE_EXISTING can also briefly surface
// ERROR_FILE_NOT_FOUND on the destination between delete-and-replace;
// retry that the same way (see TestConcurrentSaveCritJSON_NoCorruption).
// 10 attempts with 1→50ms backoff covers any realistic crit workload.
func ReadFileShared(path string) ([]byte, error) {
	const maxAttempts = 10
	delay := 1 * time.Millisecond
	var (
		data []byte
		err  error
	)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		data, err = os.ReadFile(path)
		if err == nil || !isWindowsTransientIOErr(err) {
			return data, err
		}
		time.Sleep(delay)
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
	return data, err
}

// isWindowsTransientIOErr reports Windows errors that can appear briefly
// during concurrent rename/read of the same path and are worth retrying.
func isWindowsTransientIOErr(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	var errno windows.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_LOCK_VIOLATION:
		return true
	}
	return false
}
