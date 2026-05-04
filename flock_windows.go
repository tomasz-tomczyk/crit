//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// Windows file-locking semantics differ from Unix flock: locks are mandatory
// (not advisory) and apply to byte ranges. We emulate flock by locking the
// first byte of the file. The lock is automatically released when the handle
// is closed, so it is also released when the process exits — matching the
// flock(2) safety guarantee callers depend on for stale-lock cleanup.

const (
	// Lock the first byte. Using offset 0 length 1 is the conventional
	// emulation of whole-file flock on Windows.
	lockOffsetLow  uint32 = 0
	lockOffsetHigh uint32 = 0
	lockLenLow     uint32 = 1
	lockLenHigh    uint32 = 0
)

func flockExclusive(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, lockLenLow, lockLenHigh, ol)
}

func flockExclusiveNB(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, lockLenLow, lockLenHigh, ol)
}

func funlock(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, lockLenLow, lockLenHigh, ol)
}
