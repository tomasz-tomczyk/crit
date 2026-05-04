//go:build !windows

package main

import "os"

// renameAtomic replaces dst with src atomically. On POSIX, os.Rename is the
// atomic primitive — no retry is needed.
func renameAtomic(src, dst string) error {
	return os.Rename(src, dst)
}

// readFileShared reads path. On POSIX a reader never conflicts with a
// concurrent rename, so this is just os.ReadFile.
func readFileShared(path string) ([]byte, error) {
	return os.ReadFile(path)
}
