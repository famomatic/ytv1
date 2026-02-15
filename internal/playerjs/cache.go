package playerjs

import (
	"sync"
	"time"
)

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
	return item.body, true
}

func (c *memoryCache) Set(playerID string, jsBody string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[playerID] = cacheItem{
		body:      jsBody,
		createdAt: time.Now(),
	}
}
