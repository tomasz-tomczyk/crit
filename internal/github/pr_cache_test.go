package github

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestCache builds an isolated cache wired to a counting fetchFn so the
// production singleton is untouched between tests.
func newTestCache(fn func(int) (*PRInfo, error)) *prMetadataCache {
	return &prMetadataCache{
		entries: make(map[int]*prCacheEntry),
		cap:     prMetadataCacheCap,
		fetchFn: fn,
	}
}

func TestPRMetadataCache_FirstGetIsMiss(t *testing.T) {
	var calls int32
	c := newTestCache(func(num int) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: num, Title: "first"}, nil
	})

	info, err := c.get(42)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Number != 42 || info.Title != "first" {
		t.Errorf("got %+v want Number=42 Title=first", info)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fetchFn calls=%d want 1", got)
	}
}

func TestPRMetadataCache_SecondGetIsHit(t *testing.T) {
	var calls int32
	c := newTestCache(func(num int) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: num}, nil
	})

	if _, err := c.get(42); err != nil {
		t.Fatal(err)
	}
	if _, err := c.get(42); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fetchFn calls=%d want 1 (second get must be a hit)", got)
	}
}

func TestPRMetadataCache_DistinctPRsAreIndependent(t *testing.T) {
	var calls int32
	c := newTestCache(func(num int) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: num}, nil
	})

	if _, err := c.get(1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.get(2); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("fetchFn calls=%d want 2 (distinct PRs each miss)", got)
	}
}

func TestPRMetadataCache_InvalidateRemovesEntry(t *testing.T) {
	var calls int32
	c := newTestCache(func(num int) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: num}, nil
	})

	if _, err := c.get(42); err != nil {
		t.Fatal(err)
	}
	c.invalidate(42)
	if _, err := c.get(42); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("fetchFn calls=%d want 2 (invalidate must force re-fetch)", got)
	}
}

func TestPRMetadataCache_InvalidateMissingNumberIsNoOp(t *testing.T) {
	c := newTestCache(func(int) (*PRInfo, error) { return &PRInfo{}, nil })
	c.invalidate(99) // must not panic
}

func TestPRMetadataCache_FetchErrorNotCached(t *testing.T) {
	var calls int32
	wantErr := errors.New("boom")
	c := newTestCache(func(num int) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		if atomic.LoadInt32(&calls) == 1 {
			return nil, wantErr
		}
		return &PRInfo{Number: num}, nil
	})

	if _, err := c.get(7); !errors.Is(err, wantErr) {
		t.Fatalf("first get err=%v want %v", err, wantErr)
	}
	// Second call must hit fetchFn again because errors don't populate the cache.
	info, err := c.get(7)
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if info.Number != 7 {
		t.Errorf("got Number=%d want 7", info.Number)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("fetchFn calls=%d want 2 (errors must not be cached)", got)
	}
}

// TestPRMetadataCache_ConcurrentGetSinglePopulation verifies that even if N
// goroutines race past the initial miss check, only one entry survives in
// the cache — all callers converge on a single *PRInfo. We tolerate up to N
// fetchFn invocations (no full singleflight), but post-fetch deduping must
// hand back the same pointer to every caller.
func TestPRMetadataCache_ConcurrentGetSinglePopulation(t *testing.T) {
	var calls int32
	c := newTestCache(func(num int) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		// Fresh pointer each call — only post-fetch dedupe ensures convergence.
		return &PRInfo{Number: num, Title: "v"}, nil
	})

	const goroutines = 32
	results := make([]*PRInfo, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			info, err := c.get(99)
			if err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
				return
			}
			results[idx] = info
		}(i)
	}
	wg.Wait()

	first := results[0]
	if first == nil {
		t.Fatal("first result is nil")
	}
	for i, r := range results {
		if r != first {
			t.Errorf("goroutine %d returned %p, want %p (all callers must converge)", i, r, first)
		}
	}
	// Cache must hold exactly one entry post-race.
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) != 1 {
		t.Errorf("entries=%d want 1", len(c.entries))
	}
	if c.entries[99].data != first {
		t.Errorf("cached entry %p != returned %p", c.entries[99].data, first)
	}
}

func TestInvalidatePRCache_IgnoresNonPositive(t *testing.T) {
	// Snapshot then restore the singleton so we don't bleed state into other tests.
	prev := prMetaCache
	prMetaCache = newPRMetadataCache()
	t.Cleanup(func() { prMetaCache = prev })

	prMetaCache.entries[1] = &prCacheEntry{data: &PRInfo{Number: 1}, access: time.Now()}
	InvalidatePRCache(0)
	InvalidatePRCache(-5)
	if _, ok := prMetaCache.entries[1]; !ok {
		t.Error("InvalidatePRCache(0/-5) wrongly dropped unrelated entries")
	}
	InvalidatePRCache(1)
	if _, ok := prMetaCache.entries[1]; ok {
		t.Error("InvalidatePRCache(1) failed to drop entry")
	}
}

// TestFetchPRByNumber_RoutesThroughCache verifies the package-level wiring:
// fetchPRByNumber consults prMetaCache before invoking fetchPRByNumberFn, so
// repeated calls with the same number hit `gh` exactly once.
func TestFetchPRByNumber_RoutesThroughCache(t *testing.T) {
	var calls int32
	withFetchPRByNumber(t, func(num int) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: num, Title: "cached"}, nil
	})

	for i := 0; i < 5; i++ {
		info, err := fetchPRByNumber(100)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if info.Title != "cached" {
			t.Errorf("iter %d: title=%q want cached", i, info.Title)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fetchPRByNumberFn calls=%d want 1 across 5 fetchPRByNumber calls", got)
	}

	InvalidatePRCache(100)
	if _, err := fetchPRByNumber(100); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("after invalidate, calls=%d want 2", got)
	}
}

// TestPRMetadataCache_EvictsOldestWhenFull populates the cache past its cap
// and verifies that the LRU entry is evicted while the most-recently-touched
// entries survive.
func TestPRMetadataCache_EvictsOldestWhenFull(t *testing.T) {
	c := &prMetadataCache{
		entries: make(map[int]*prCacheEntry),
		cap:     3,
		fetchFn: func(num int) (*PRInfo, error) { return &PRInfo{Number: num}, nil },
	}

	// Fill exactly to cap.
	for i := 1; i <= 3; i++ {
		if _, err := c.get(i); err != nil {
			t.Fatal(err)
		}
		// Stagger access times so LRU ordering is unambiguous.
		time.Sleep(2 * time.Millisecond)
	}
	// Touch PR 1 so PR 2 becomes the LRU.
	if _, err := c.get(1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)

	// Inserting PR 4 must evict PR 2 (the now-oldest).
	if _, err := c.get(4); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) != 3 {
		t.Errorf("entries=%d want 3 (cap)", len(c.entries))
	}
	if _, ok := c.entries[2]; ok {
		t.Errorf("PR 2 should have been evicted (LRU); entries=%v", keys(c.entries))
	}
	for _, want := range []int{1, 3, 4} {
		if _, ok := c.entries[want]; !ok {
			t.Errorf("PR %d should be retained; entries=%v", want, keys(c.entries))
		}
	}
}

func keys(m map[int]*prCacheEntry) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
