package policy

import (
	"github.com/mjmst/ytv1/internal/innertube"
)

// Selector decides which clients to use for a given video request.
type Selector interface {
	Select(videoID string) []innertube.ClientProfile
	Registry() innertube.Registry
}

type defaultSelector struct {
	registry innertube.Registry
}

func NewSelector(registry innertube.Registry) Selector {
	return &defaultSelector{
		registry: registry,
	}
}

func (s *defaultSelector) Registry() innertube.Registry {
	return s.registry
}

func (s *defaultSelector) Select(videoID string) []innertube.ClientProfile {
	// Default strategy: Try Android, iOS, Web, and TV.
	// In the future, this can be smarter (e.g. check if video is age-gated, music, etc.)
	
	clients := []string{
		"android",
		"ios",
		"web",
		// "tv", // TV client often requires different handling, maybe add later as fallback
	}

	var profiles []innertube.ClientProfile
	for _, name := range clients {
		if p, ok := s.registry.Get(name); ok {
			profiles = append(profiles, p)
		}
	}
	
	return profiles
}
