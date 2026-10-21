package session

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
	if err := AtomicWriteFile(target, []byte("payload"), 0o644); err == nil {
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

func TestReplaceInProgress(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "review.json")
	if replaceInProgress(target) {
		t.Fatal("empty folder reported a replace in progress")
	}
	if replaceInProgress(filepath.Join(dir, "missing", "review.json")) {
		t.Fatal("missing folder reported a replace in progress")
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshots.json123.tmp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if replaceInProgress(target) {
		t.Fatal("another file's temp file counted for review.json")
	}
	if err := os.WriteFile(filepath.Join(dir, "review.json123.tmp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !replaceInProgress(target) {
		t.Fatal("temp file for review.json not detected")
	}
}
