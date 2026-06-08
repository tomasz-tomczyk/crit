package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Success / perms / overwrite / dir-creation are already covered in
// daemon_test.go (TestAtomicWriteFile_*). This file adds the missing failure
// path: a write into a read-only parent must surface an error AND leave no
// .tmp files behind.
func TestAtomicWriteFile_FailureLeavesNoTmpFiles(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("read-only-dir failure path needs non-root POSIX")
	}
	root := t.TempDir()
	readOnly := filepath.Join(root, "ro")
	if err := os.MkdirAll(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })

	target := filepath.Join(readOnly, "x")
	if err := atomicWriteFile(target, []byte("payload"), 0o644); err == nil {
		t.Fatal("expected error writing into read-only directory, got nil")
	}
	entries, err := os.ReadDir(readOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("read-only dir should be empty, found %s", e.Name())
	}
}
