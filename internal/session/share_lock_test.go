package session

import (
	"os"
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
