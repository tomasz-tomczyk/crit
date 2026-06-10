package session

import (
	"container/list"
	"sync"
)

// remoteFileCacheCap caps the number of (sha, path) -> []byte entries held in
// Session.remoteFileCache. 256 is comfortably above the typical PR file count
// (median ~5, P99 well under 100) so cache hits dominate, while keeping
// worst-case memory bounded for long-lived daemons that switch between many
// large PRs.
const remoteFileCacheCap = 256

// bytesLRU is a simple, mutex-protected LRU cache mapping string keys to
// []byte values. Used for Session.remoteFileCache to bound memory growth in
// long-lived daemons.
//
// Implementation: container/list doubly-linked list for O(1) move-to-front,
// plus a map for O(1) lookup. Eviction removes the back of the list.
type bytesLRU struct {
	mu    sync.Mutex
	cap   int
	ll    *list.List
	index map[string]*list.Element
}

type lruItem struct {
	key string
	val []byte
}

// newBytesLRU constructs a bytesLRU with the given capacity. Capacity must
// be > 0; values <= 0 are clamped to 1 to avoid divide-by-zero-style issues
// in the eviction loop.
func newBytesLRU(cap int) *bytesLRU {
	if cap <= 0 {
		cap = 1
	}
	return &bytesLRU{
		cap:   cap,
		ll:    list.New(),
		index: make(map[string]*list.Element, cap),
	}
}

// Get returns the value for key and reports whether it was present. Touching
// an entry promotes it to most-recently-used.
func (c *bytesLRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.index[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(e)
	return e.Value.(*lruItem).val, true
}

// Put inserts or updates key -> val, promoting the entry to most-recently-used
// and evicting the least-recently-used entry when the cache is over capacity.
func (c *bytesLRU) Put(key string, val []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.index[key]; ok {
		e.Value.(*lruItem).val = val
		c.ll.MoveToFront(e)
		return
	}
	e := c.ll.PushFront(&lruItem{key: key, val: val})
	c.index[key] = e
	for c.ll.Len() > c.cap {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		c.ll.Remove(oldest)
		delete(c.index, oldest.Value.(*lruItem).key)
	}
}

// Len returns the current entry count. Mainly useful for tests.
func (c *bytesLRU) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
