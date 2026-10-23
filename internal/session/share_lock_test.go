package session

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithShareLock_SerializesConcurrentCallers(t *testing.T) {
	dir := t.TempDir()
	identity := dir

	var active atomic.Int32
	var peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithShareLock(identity, func() error {
				n := active.Add(1)
				if cur := peak.Load(); n > cur {
					peak.Store(n)
				}
				time.Sleep(20 * time.Millisecond)
				active.Add(-1)
				return nil
			})
			if err != nil {
				t.Errorf("WithShareLock: %v", err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Errorf("expected peak concurrency 1, got %d", peak.Load())
	}
	if _, err := os.Stat(ReviewPathsFor(identity).ShareLock); err != nil {
		t.Errorf("share.lock not created: %v", err)
	}
}

func TestWithShareLock_PropagatesFnError(t *testing.T) {
	want := errors.New("boom")
	got := WithShareLock(t.TempDir(), func() error { return want })
	if !errors.Is(got, want) {
		t.Fatalf("WithShareLock() = %v, want %v", got, want)
	}
}

// A share that waited while ClearReviewFolder removed share.lock locks a
// fresh lock file, so it still excludes a share that starts after the
// removal.
func TestShareLockExcludesAfterFolderRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove a lock file that is open")
	}
	dir := filepath.Join(t.TempDir(), "review")
	secondIn := make(chan struct{})
	secondRelease := make(chan struct{})
	second := make(chan error, 1)
	err := WithShareLock(dir, func() error {
		go func() {
			second <- WithShareLock(dir, func() error {
				close(secondIn)
				<-secondRelease
				return nil
			})
		}()
		time.Sleep(50 * time.Millisecond) // let the second share wait on this lock file
		return ClearReviewFolder(dir)
	})
	if err != nil {
		t.Fatal(err)
	}
	<-secondIn

	thirdIn := make(chan struct{})
	third := make(chan error, 1)
	go func() {
		third <- WithShareLock(dir, func() error {
			close(thirdIn)
			return nil
		})
	}()
	select {
	case <-thirdIn:
		t.Fatal("third share entered while the second held the share lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(secondRelease)
	for _, ch := range []chan error{second, third} {
		if err := <-ch; err != nil {
			t.Fatal(err)
		}
	}
}
