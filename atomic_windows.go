//go:build windows

package main

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
		if !isWindowsSharingViolation(err) {
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
// 10 attempts with 1→50ms backoff covers any realistic crit workload.
func readFileShared(path string) ([]byte, error) {
	const maxAttempts = 10
	delay := 1 * time.Millisecond
	var (
		data []byte
		err  error
	)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		data, err = os.ReadFile(path)
		if err == nil || !isWindowsSharingViolation(err) {
			return data, err
		}
		time.Sleep(delay)
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
	return data, err
}

func isWindowsSharingViolation(err error) bool {
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
