package github

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

// newTestCache builds an isolated cache wired to a counting fetchFn so the
// production singleton is untouched between tests.
func newTestCache(fn func(forge.ChangeID) (*PRInfo, error)) *prMetadataCache {
	return &prMetadataCache{
		entries: make(map[string]*prCacheEntry),
		cap:     prMetadataCacheCap,
		fetchFn: fn,
	}
}

func numID(n int) forge.ChangeID { return forge.ChangeID{Number: n} }

func TestPRMetadataCache_FirstGetIsMiss(t *testing.T) {
	var calls int32
	c := newTestCache(func(id forge.ChangeID) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: id.Number, Title: "first"}, nil
	})

	info, err := c.get(numID(42))
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
	c := newTestCache(func(id forge.ChangeID) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: id.Number}, nil
	})

	if _, err := c.get(numID(42)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.get(numID(42)); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fetchFn calls=%d want 1 (second get must be a hit)", got)
	}
}

func TestPRMetadataCache_DistinctPRsAreIndependent(t *testing.T) {
	var calls int32
	c := newTestCache(func(id forge.ChangeID) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: id.Number}, nil
	})

	if _, err := c.get(numID(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.get(numID(2)); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("fetchFn calls=%d want 2 (distinct PRs each miss)", got)
	}
}

func TestPRMetadataCache_SameNumberDifferentProjectsAreIndependent(t *testing.T) {
	var calls int32
	c := newTestCache(func(id forge.ChangeID) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: id.Number, URL: "https://github.com/" + id.Project + "/pull/" + strconv.Itoa(id.Number)}, nil
	})

	a := forge.ChangeID{Number: 1, Project: "org/repo-a"}
	b := forge.ChangeID{Number: 1, Project: "org/repo-b"}
	infoA, err := c.get(a)
	if err != nil {
		t.Fatal(err)
	}
	infoB, err := c.get(b)
	if err != nil {
		t.Fatal(err)
	}
	if infoA.URL == infoB.URL {
		t.Fatalf("same-number PRs in different repos collided: %q", infoA.URL)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("fetchFn calls=%d want 2", got)
	}
}

func TestPRMetadataCache_InvalidateRemovesEntry(t *testing.T) {
	var calls int32
	c := newTestCache(func(id forge.ChangeID) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: id.Number}, nil
	})

	if _, err := c.get(numID(42)); err != nil {
		t.Fatal(err)
	}
	c.invalidate(numID(42))
	if _, err := c.get(numID(42)); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("fetchFn calls=%d want 2 (invalidate must force re-fetch)", got)
	}
}

func TestPRMetadataCache_InvalidateMissingNumberIsNoOp(t *testing.T) {
	c := newTestCache(func(forge.ChangeID) (*PRInfo, error) { return &PRInfo{}, nil })
	c.invalidate(numID(99)) // must not panic
}

func TestPRMetadataCache_FetchErrorNotCached(t *testing.T) {
	var calls int32
	wantErr := errors.New("boom")
	c := newTestCache(func(id forge.ChangeID) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		if atomic.LoadInt32(&calls) == 1 {
			return nil, wantErr
		}
		return &PRInfo{Number: id.Number}, nil
	})

	if _, err := c.get(numID(7)); !errors.Is(err, wantErr) {
		t.Fatalf("first get err=%v want %v", err, wantErr)
	}
	info, err := c.get(numID(7))
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

func TestPRMetadataCache_ConcurrentGetSinglePopulation(t *testing.T) {
	var calls int32
	c := newTestCache(func(id forge.ChangeID) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: id.Number, Title: "v"}, nil
	})

	const goroutines = 32
	results := make([]*PRInfo, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			info, err := c.get(numID(99))
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
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) != 1 {
		t.Errorf("entries=%d want 1", len(c.entries))
	}
	if c.entries["99"].data != first {
		t.Errorf("cached entry %p != returned %p", c.entries["99"].data, first)
	}
}

func TestInvalidatePR_DropsProjectQualifiedKey(t *testing.T) {
	prev := prMetaCache
	prMetaCache = newPRMetadataCache()
	t.Cleanup(func() { prMetaCache = prev })

	id := forge.ChangeID{Number: 1, Project: "org/repo-b"}
	prMetaCache.entries[prCacheKey(id)] = &prCacheEntry{data: &PRInfo{Number: 1}, access: time.Now()}
	prMetaCache.entries["1"] = &prCacheEntry{data: &PRInfo{Number: 1, Title: "bare"}, access: time.Now()}

	InvalidatePR(id)
	if _, ok := prMetaCache.entries[prCacheKey(id)]; ok {
		t.Error("InvalidatePR failed to drop project-qualified entry")
	}
	if _, ok := prMetaCache.entries["1"]; !ok {
		t.Error("InvalidatePR wrongly dropped bare-number entry")
	}
}

func TestProviderInvalidate_DropsBareAndQualified(t *testing.T) {
	prev := prMetaCache
	prMetaCache = newPRMetadataCache()
	t.Cleanup(func() { prMetaCache = prev })

	id := forge.ChangeID{Number: 3, Project: "org/repo-b"}
	prMetaCache.entries[prCacheKey(id)] = &prCacheEntry{data: &PRInfo{Number: 3}, access: time.Now()}
	prMetaCache.entries["3"] = &prCacheEntry{data: &PRInfo{Number: 3}, access: time.Now()}
	prMetaCache.entries["4"] = &prCacheEntry{data: &PRInfo{Number: 4}, access: time.Now()}

	Provider{}.Invalidate(id)
	if _, ok := prMetaCache.entries[prCacheKey(id)]; ok {
		t.Error("Provider.Invalidate left project-qualified entry")
	}
	if _, ok := prMetaCache.entries["3"]; ok {
		t.Error("Provider.Invalidate left bare entry")
	}
	if _, ok := prMetaCache.entries["4"]; !ok {
		t.Error("Provider.Invalidate wrongly dropped unrelated PR")
	}
}

func TestInvalidatePRCache_DropsBareAndQualified(t *testing.T) {
	prev := prMetaCache
	prMetaCache = newPRMetadataCache()
	t.Cleanup(func() { prMetaCache = prev })

	id := forge.ChangeID{Number: 7, Project: "org/repo-b", Host: "github.example.com"}
	prMetaCache.entries[prCacheKey(id)] = &prCacheEntry{data: &PRInfo{Number: 7}, access: time.Now()}
	prMetaCache.entries["7"] = &prCacheEntry{data: &PRInfo{Number: 7}, access: time.Now()}
	prMetaCache.entries["8"] = &prCacheEntry{data: &PRInfo{Number: 8}, access: time.Now()}

	InvalidatePRCache(7)
	if _, ok := prMetaCache.entries[prCacheKey(id)]; ok {
		t.Error("InvalidatePRCache left project-qualified entry")
	}
	if _, ok := prMetaCache.entries["7"]; ok {
		t.Error("InvalidatePRCache left bare entry")
	}
	if _, ok := prMetaCache.entries["8"]; !ok {
		t.Error("InvalidatePRCache wrongly dropped unrelated PR")
	}
}

func TestInvalidatePRCache_IgnoresNonPositive(t *testing.T) {
	prev := prMetaCache
	prMetaCache = newPRMetadataCache()
	t.Cleanup(func() { prMetaCache = prev })

	prMetaCache.entries["1"] = &prCacheEntry{data: &PRInfo{Number: 1}, access: time.Now()}
	InvalidatePRCache(0)
	InvalidatePRCache(-5)
	if _, ok := prMetaCache.entries["1"]; !ok {
		t.Error("InvalidatePRCache(0/-5) wrongly dropped unrelated entries")
	}
	InvalidatePRCache(1)
	if _, ok := prMetaCache.entries["1"]; ok {
		t.Error("InvalidatePRCache(1) failed to drop entry")
	}
}

func TestFetchPRByNumber_RoutesThroughCache(t *testing.T) {
	var calls int32
	withFetchPRByNumber(t, func(num int) (*PRInfo, error) {
		atomic.AddInt32(&calls, 1)
		return &PRInfo{Number: num, Title: "cached"}, nil
	})

	for i := 0; i < 5; i++ {
		info, err := FetchPRByNumber(100)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if info.Title != "cached" {
			t.Errorf("iter %d: title=%q want cached", i, info.Title)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fetchPRFn calls=%d want 1 across 5 FetchPRByNumber calls", got)
	}

	InvalidatePRCache(100)
	if _, err := FetchPRByNumber(100); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("after invalidate, calls=%d want 2", got)
	}
}

func TestFetchPR_RejectsNonPositiveNumber(t *testing.T) {
	if _, err := FetchPR(forge.ChangeID{}); err == nil || !strings.Contains(err.Error(), "invalid PR number") {
		t.Fatalf("FetchPR(0) err = %v", err)
	}
	if _, err := FetchPR(forge.ChangeID{Number: -1}); err == nil {
		t.Fatal("FetchPR(-1) unexpectedly succeeded")
	}
}

func TestPRMetadataCache_EvictsOldestWhenFull(t *testing.T) {
	c := &prMetadataCache{
		entries: make(map[string]*prCacheEntry),
		cap:     3,
		fetchFn: func(id forge.ChangeID) (*PRInfo, error) { return &PRInfo{Number: id.Number}, nil },
	}

	for i := 1; i <= 3; i++ {
		if _, err := c.get(numID(i)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := c.get(numID(1)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)

	if _, err := c.get(numID(4)); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) != 3 {
		t.Errorf("entries=%d want 3 (cap)", len(c.entries))
	}
	if _, ok := c.entries["2"]; ok {
		t.Errorf("PR 2 should have been evicted (LRU); entries=%v", keys(c.entries))
	}
	for _, want := range []string{"1", "3", "4"} {
		if _, ok := c.entries[want]; !ok {
			t.Errorf("PR %s should be retained; entries=%v", want, keys(c.entries))
		}
	}
}

func keys(m map[string]*prCacheEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
