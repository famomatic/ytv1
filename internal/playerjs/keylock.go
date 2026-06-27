package playerjs

import "sync"

// keyLock serializes operations that share a logical key. It keeps one
// *sync.Mutex per live key and reference-counts entries so the map stays
// bounded. Used by the player JS resolver to coalesce concurrent fetches
// for the same player cache key so only one network call runs at a time.
type keyLock struct {
	mu    sync.Mutex
	locks map[string]*lockEntry
}

type lockEntry struct {
	mu      sync.Mutex
	waiters int
}

func newKeyLock() *keyLock {
	return &keyLock{locks: make(map[string]*lockEntry)}
}

func (l *keyLock) acquire(key string) (*sync.Mutex, func()) {
	l.mu.Lock()
	entry, ok := l.locks[key]
	if !ok {
		entry = &lockEntry{}
		l.locks[key] = entry
	}
	entry.waiters++
	l.mu.Unlock()

	once := false
	release := func() {
		if once {
			return
		}
		once = true
		l.mu.Lock()
		entry.waiters--
		if entry.waiters == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
	return &entry.mu, release
}
