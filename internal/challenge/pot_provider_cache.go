package challenge

import (
	"context"
	"strings"
	"sync"

	"github.com/famomatic/ytv1/internal/innertube"
)

type cachedPoTokenProvider struct {
	base  innertube.PoTokenProvider
	mu    sync.RWMutex
	cache map[string]string
}

// NewCachedPoTokenProvider wraps a PoTokenProvider with in-memory client-keyed
// token caching. Empty tokens are not cached.
func NewCachedPoTokenProvider(base innertube.PoTokenProvider) innertube.PoTokenProvider {
	if base == nil {
		return nil
	}
	return &cachedPoTokenProvider{
		base:  base,
		cache: make(map[string]string),
	}
}

func (p *cachedPoTokenProvider) GetToken(ctx context.Context, clientID string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(clientID))
	if key == "" {
		return p.base.GetToken(ctx, clientID)
	}

	p.mu.RLock()
	if token, ok := p.cache[key]; ok && token != "" {
		p.mu.RUnlock()
		return token, nil
	}
	p.mu.RUnlock()

	token, err := p.base.GetToken(ctx, clientID)
	if err != nil || strings.TrimSpace(token) == "" {
		return token, err
	}

	p.mu.Lock()
	p.cache[key] = token
	p.mu.Unlock()
	return token, nil
}
