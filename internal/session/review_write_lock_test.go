package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWithReviewWriteLockRejectsInvalidPaths(t *testing.T) {
	t.Run("invalid identity", func(t *testing.T) {
		err := WithReviewWriteLock(filepath.Join(t.TempDir(), "invalid\x00"), func() error {
			t.Error("callback ran without a lock")
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "stat review identity") {
			t.Fatalf("expected identity error, got %v", err)
		}
	})
	t.Run("lock path is a directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "review.lock"), 0o700); err != nil {
			t.Fatal(err)
		}
		err := WithReviewWriteLock(dir, func() error {
			t.Error("callback ran without a lock")
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "opening review lock") {
			t.Fatalf("expected lock-file error, got %v", err)
		}
	})
}

func TestWithReviewWriteLockCannotCreateFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix permission enforcement")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(parent, 0o700); err != nil {
			t.Error(err)
		}
	})
	err := WithReviewWriteLock(filepath.Join(parent, "review"), func() error {
		t.Error("callback ran without a lock")
		return nil
	})
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "mkdir" || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("expected mkdir permission error, got %v", err)
	}
}

func TestWithReviewWriteLockReleasesAfterCallbackError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "review")
	want := errors.New("write failed")
	if err := WithReviewWriteLock(dir, func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("callback error lost: got %v, want %v", err, want)
	}
	// Use another descriptor to verify that the failed transaction released its lock.
	done := make(chan error, 1)
	go func() { done <- WithReviewWriteLock(dir, func() error { return nil }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock remained held after callback error")
	}
}

func TestWithReviewWriteLockExcludesSecondHolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "review")
	held := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- WithReviewWriteLock(dir, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	entered := make(chan struct{})
	second := make(chan error, 1)
	go func() {
		second <- WithReviewWriteLock(dir, func() error {
			close(entered)
			return nil
		})
	}()
	select {
	case <-entered:
		t.Fatal("second holder entered while the first held the lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
}

// Removing a review waits for a writer holding the lock, so the writer's
// save cannot bring the removed review back.
func TestClearReviewFolderWaitsForLockHolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "review")
	if err := SaveCritJSON(dir, CritJSON{ReviewComments: []Comment{{ID: "r1"}}}); err != nil {
		t.Fatal(err)
	}
	held := make(chan struct{})
	release := make(chan struct{})
	writer := make(chan error, 1)
	go func() {
		writer <- WithReviewWriteLock(dir, func() error {
			close(held)
			<-release
			return SaveCritJSON(dir, CritJSON{ReviewComments: []Comment{{ID: "r1"}, {ID: "r2"}}})
		})
	}()
	<-held
	cleared := make(chan error, 1)
	go func() { cleared <- ClearReviewFolder(dir) }()
	select {
	case <-cleared:
		t.Fatal("ClearReviewFolder ran while a writer held the lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-writer; err != nil {
		t.Fatal(err)
	}
	if err := <-cleared; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("review folder survived: %v", err)
	}
}

// A writer that waited while the folder was removed locks a fresh lock
// file, so it still excludes a writer that arrives after the removal.
func TestReviewLockExcludesAfterFolderRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove a lock file that is open")
	}
	dir := filepath.Join(t.TempDir(), "review")
	secondIn := make(chan struct{})
	secondRelease := make(chan struct{})
	second := make(chan error, 1)
	err := WithReviewWriteLock(dir, func() error {
		go func() {
			second <- WithReviewWriteLock(dir, func() error {
				close(secondIn)
				<-secondRelease
				return nil
			})
		}()
		time.Sleep(50 * time.Millisecond) // let the second writer wait on this lock file
		return os.RemoveAll(dir)          // what ClearReviewFolder does on Unix
	})
	if err != nil {
		t.Fatal(err)
	}
	<-secondIn

	thirdIn := make(chan struct{})
	third := make(chan error, 1)
	go func() {
		third <- WithReviewWriteLock(dir, func() error {
			close(thirdIn)
			return nil
		})
	}()
	select {
	case <-thirdIn:
		t.Fatal("third writer entered while the second held the review lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(secondRelease)
	for _, ch := range []chan error{second, third} {
		if err := <-ch; err != nil {
			t.Fatal(err)
		}
	}
}
