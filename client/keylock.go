package client

import (
	"github.com/famomatic/ytv1/internal/ctxsync"
	"sync"
)

// keyLock serializes operations that share a logical key (for example a
// video ID). It prevents thundering-herd duplicate network fetches when
// multiple goroutines request the same resource concurrently while still
// allowing unrelated keys to proceed in parallel.
//
// The implementation keeps one *sync.Mutex per live key. Entries are
// reference-counted: the mutex is removed once no goroutine holds it,
// so the map stays bounded across distinct keys.
type keyLock struct {
	mu    sync.Mutex
	locks map[string]*lockEntry
}

type lockEntry struct {
	mu      ctxsync.Mutex
	waiters int
}

func newKeyLock() *keyLock {
	return &keyLock{locks: make(map[string]*lockEntry)}
}

// acquire returns a mutex for the given key. The caller must call the
// returned release function exactly once when done (typically via defer).
func (l *keyLock) acquire(key string) (*ctxsync.Mutex, func()) {
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
