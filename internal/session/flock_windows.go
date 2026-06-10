//go:build windows

package session

import (
	"os"

	"golang.org/x/sys/windows"
)

const (
	lockOffsetLow  uint32 = 0
	lockOffsetHigh uint32 = 0
	lockLenLow     uint32 = 1
	lockLenHigh    uint32 = 0
)

func flockExclusive(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, lockLenLow, lockLenHigh, ol)
}

func funlock(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, lockLenLow, lockLenHigh, ol)
}
