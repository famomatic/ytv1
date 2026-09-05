package challenge

import (
	"context"
	"github.com/famomatic/ytv1/internal/ctxsync"
	"strings"
	"sync"
	"time"

	"github.com/famomatic/ytv1/internal/innertube"
)

type cachedPoTokenProvider struct {
	ttl     time.Duration
	expires map[string]time.Time
	base    innertube.PoTokenProvider
	mu      sync.RWMutex
	cache   map[string]string
	// fetchMu serializes concurrent cold fetches per client id so a burst of
	// cache-miss callers issues a single upstream GetToken instead of a
	// thundering herd of identical network requests.
	fetchMu map[string]*ctxsync.Mutex
	fetchGu sync.Mutex
}

// NewCachedPoTokenProvider wraps a PoTokenProvider with in-memory client-keyed
// token caching. Empty tokens are not cached.
func NewCachedPoTokenProvider(base innertube.PoTokenProvider) innertube.PoTokenProvider {
	return NewCachedPoTokenProviderWithTTL(base, 5*time.Minute)
}
func NewCachedPoTokenProviderWithTTL(base innertube.PoTokenProvider, ttl time.Duration) innertube.PoTokenProvider {
	if ttl < 0 {
		return base
	}
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	if base == nil {
		return nil
	}
	return &cachedPoTokenProvider{
		base: base, ttl: ttl, expires: make(map[string]time.Time),
		cache:   make(map[string]string),
		fetchMu: make(map[string]*ctxsync.Mutex),
	}
}

func (p *cachedPoTokenProvider) GetToken(ctx context.Context, clientID string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(clientID))
	if key == "" {
		return p.base.GetToken(ctx, clientID)
	}

	if token, ok := p.load(key); ok {
		return token, nil
	}

	// Serialize cold fetches for this key so only one upstream call runs.
	lock := p.fetchLock(key)
	if err := lock.LockContext(ctx); err != nil {
		return "", err
	}
	defer lock.Unlock()

	// Re-check under the per-key lock: a concurrent fetch may have populated it.
	if token, ok := p.load(key); ok {
		return token, nil
	}

	token, err := p.base.GetToken(ctx, clientID)
	if err != nil || strings.TrimSpace(token) == "" {
		return token, err
	}

	p.mu.Lock()
	p.cache[key] = token
	p.expires[key] = time.Now().Add(p.ttl)
	p.mu.Unlock()
	return token, nil
}

func (p *cachedPoTokenProvider) load(key string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	token, ok := p.cache[key]
	return token, ok && token != "" && time.Now().Before(p.expires[key])
}

func (p *cachedPoTokenProvider) fetchLock(key string) *ctxsync.Mutex {
	p.fetchGu.Lock()
	defer p.fetchGu.Unlock()
	lock, ok := p.fetchMu[key]
	if !ok {
		lock = &ctxsync.Mutex{}
		p.fetchMu[key] = lock
	}
	return lock
}
