package innertube

import "sync"

type defaultRegistry struct {
	clients map[string]ClientProfile
	mu      sync.RWMutex
}

// NewRegistry creates a new registry with default clients.
func NewRegistry() Registry {
	return &defaultRegistry{
		clients: map[string]ClientProfile{
			"web":          WebClient,
			"web_embedded": WebEmbeddedClient,
			"mweb":         MWebClient,
			"android":      AndroidClient,
			"ios":          iOSClient,
			"tv":           TVClient,
		},
	}
}

func (r *defaultRegistry) Get(name string) (ClientProfile, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[name]
	return c, ok
}

func (r *defaultRegistry) All() []ClientProfile {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	all := make([]ClientProfile, 0, len(r.clients))
	for _, c := range r.clients {
		all = append(all, c)
	}
	return all
}
