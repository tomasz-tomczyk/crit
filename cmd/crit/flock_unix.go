//go:build !windows

package main

import (
	"os"
	"syscall"
)

// flockExclusive acquires an exclusive advisory lock on f, blocking until the
// lock is available. Released automatically when the process dies.
func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// flockExclusiveNB acquires an exclusive advisory lock on f without blocking.
// Returns an error immediately if another process holds the lock.
func flockExclusiveNB(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// funlock releases an advisory lock previously acquired on f.
func funlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
