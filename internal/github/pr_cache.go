package github

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

// prMetadataCacheCap caps prMetadataCache size to bound memory growth in
// long-lived daemons that switch between many PRs. 64 entries is generous —
// even an active reviewer juggling a stack rarely revisits more than a dozen
// PRs in one daemon lifetime, and PRInfo is a small struct.
const prMetadataCacheCap = 64

// prCacheEntry pairs cached PRInfo with its last-access time so the cache can
// evict the least-recently-used entry when full.
type prCacheEntry struct {
	data   *PRInfo
	access time.Time
}

// prMetadataCache memoizes PRInfo lookups by ChangeID (number, optionally
// qualified by owner/repo) for the lifetime of the daemon process. Switching
// back to a previously-visited PR feels instant when this metadata is held in
// memory; the alternative is a 1-3s `gh pr view` round trip on every focus
// change. Capacity-bounded LRU; entries also invalidate on force-push and
// `crit pull`.
type prMetadataCache struct {
	mu      sync.Mutex
	entries map[string]*prCacheEntry
	cap     int

	// fetchFn fetches a PR on cache miss. Indirected so tests can drive the
	// cache without shelling `gh`. Defaults to fetchPRFn (the same
	// indirection point the rest of the codebase uses for test stubs).
	fetchFn func(forge.ChangeID) (*PRInfo, error)
}

// newPRMetadataCache constructs a cache wired to the live fetcher.
func newPRMetadataCache() *prMetadataCache {
	return &prMetadataCache{
		entries: make(map[string]*prCacheEntry),
		cap:     prMetadataCacheCap,
		fetchFn: func(id forge.ChangeID) (*PRInfo, error) { return fetchPRFn(id) },
	}
}

// get returns the cached PRInfo for id, populating it on miss. Concurrent
// callers requesting the same uncached PR may all observe the miss and call
// fetchFn — the second-arrival check after the fetch ensures the map only
// stores the first successful result, so callers see a consistent value
// without the complexity of a full singleflight.Group. The duplicate `gh`
// invocation is acceptable: focus switches are user-driven and rarely hit the
// same uncached PR from multiple goroutines.
func (c *prMetadataCache) get(id forge.ChangeID) (*PRInfo, error) {
	key := prCacheKey(id)
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		e.access = time.Now()
		info := e.data
		c.mu.Unlock()
		return info, nil
	}
	c.mu.Unlock()

	info, err := c.fetchFn(id)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.entries[key]; ok {
		// Race: another goroutine populated while we were fetching.
		// Prefer the existing entry so all callers converge on one pointer.
		existing.access = time.Now()
		return existing.data, nil
	}
	if c.cap > 0 && len(c.entries) >= c.cap {
		c.evictOldestLocked()
	}
	c.entries[key] = &prCacheEntry{data: info, access: time.Now()}
	return info, nil
}

// evictOldestLocked drops the entry with the oldest access time. Caller holds c.mu.
// Linear scan is fine: cap is small (64) and evictions only happen on cache miss
// after the cap is reached.
func (c *prMetadataCache) evictOldestLocked() {
	var oldestKey string
	var oldestAt time.Time
	first := true
	for key, e := range c.entries {
		if first || e.access.Before(oldestAt) {
			oldestKey = key
			oldestAt = e.access
			first = false
		}
	}
	if !first {
		delete(c.entries, oldestKey)
	}
}

// invalidate drops the cache entry for id. Safe to call when absent.
func (c *prMetadataCache) invalidate(id forge.ChangeID) {
	c.mu.Lock()
	delete(c.entries, prCacheKey(id))
	c.mu.Unlock()
}

// invalidateNumber drops the bare-number entry and every project-qualified
// entry for num. Used when only the PR number is known (e.g. crit pull).
func (c *prMetadataCache) invalidateNumber(num int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, strconv.Itoa(num))
	suffix := "#" + strconv.Itoa(num)
	for key := range c.entries {
		if strings.HasSuffix(key, suffix) {
			delete(c.entries, key)
		}
	}
}

// reset clears the entire cache. Used by tests to isolate one fixture's
// fetchFn from the next; production code should prefer invalidate.
func (c *prMetadataCache) reset() {
	c.mu.Lock()
	c.entries = make(map[string]*prCacheEntry)
	c.mu.Unlock()
}

// prMetaCache is the package-level singleton consulted by FetchPR and
// the focus/pull invalidation hooks. Mirrors the singleton shape of
// PRListCache (held on Server) but lives at package scope because callers
// (including the CLI `crit pull` path) don't always have a Server in hand.
var prMetaCache = newPRMetadataCache()

// InvalidatePRCache drops every cached PRInfo for num — both the bare-number
// key (checkout-scoped --pr) and any project-qualified keys (URL-scoped).
// Used by `crit pull`, which only knows the PR number.
func InvalidatePRCache(num int) {
	if num <= 0 {
		return
	}
	prMetaCache.invalidateNumber(num)
}

// InvalidatePR drops the cached PRInfo for id (project-qualified when set).
func InvalidatePR(id forge.ChangeID) {
	if id.Number <= 0 {
		return
	}
	prMetaCache.invalidate(id)
}
