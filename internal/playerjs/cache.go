package playerjs

import (
	"sync"
	"time"
)

// defaultPlayerJSCacheTTL is the maximum age for cached player JS bodies.
// YouTube rotates player JS every few hours; entries older than this are
// treated as cache misses and re-fetched.
const defaultPlayerJSCacheTTL = 6 * time.Hour

// maxPlayerJSCacheEntries bounds the number of distinct player JS variants
// retained in memory to prevent unbounded growth.
const maxPlayerJSCacheEntries = 16

type Cache interface {
	Get(playerID string) (string, bool)
	Set(playerID string, jsBody string)
}

type memoryCache struct {
	mu    sync.RWMutex
	items map[string]cacheItem
}

type cacheItem struct {
	body      string
	createdAt time.Time
}

func NewMemoryCache() Cache {
	return &memoryCache{
		items: make(map[string]cacheItem),
	}
}

func (c *memoryCache) Get(playerID string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.items[playerID]
	if !ok {
		return "", false
	}
	if time.Since(item.createdAt) > defaultPlayerJSCacheTTL {
		return "", false // expired — caller will re-fetch
	}
	return item.body, true
}

func (c *memoryCache) Set(playerID string, jsBody string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpired()
	// Enforce max entries (LRU via oldest-first).
	if len(c.items) >= maxPlayerJSCacheEntries {
		var oldestKey string
		var oldest time.Time
		first := true
		for k, v := range c.items {
			if first || v.createdAt.Before(oldest) {
				oldestKey = k
				oldest = v.createdAt
				first = false
			}
		}
		if oldestKey != "" {
			delete(c.items, oldestKey)
		}
	}
	c.items[playerID] = cacheItem{
		body:      jsBody,
		createdAt: time.Now(),
	}
}

func (c *memoryCache) evictExpired() {
	for k, v := range c.items {
		if time.Since(v.createdAt) > defaultPlayerJSCacheTTL {
			delete(c.items, k)
		}
	}
}
