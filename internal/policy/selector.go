package policy

import (
	"strings"

	"github.com/famomatic/ytv1/internal/innertube"
)

// Selector decides which clients to use for a given video request.
type Selector interface {
	Select(videoID string) []innertube.ClientProfile
	Registry() innertube.Registry
}

type defaultSelector struct {
	registry    innertube.Registry
	clientOrder []string
	clientSkip  map[string]struct{}
}

func NewSelector(registry innertube.Registry, clientOrder []string, clientSkip []string) Selector {
	skip := make(map[string]struct{}, len(clientSkip))
	for _, name := range clientSkip {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			continue
		}
		skip[normalized] = struct{}{}
	}
	return &defaultSelector{
		registry:    registry,
		clientOrder: clientOrder,
		clientSkip:  skip,
	}
}

func (s *defaultSelector) Registry() innertube.Registry {
	return s.registry
}

func (s *defaultSelector) Select(videoID string) []innertube.ClientProfile {
	clients := s.clientOrder
	if len(clients) == 0 {
		// Default strategy follows yt-dlp style priority before fallback clients.
		clients = []string{
			"android_vr",
			"web",
			"web_safari",
			"android",
			"ios",
			"mweb",
			"web_embedded",
			"tv",
		}
	}

	var profiles []innertube.ClientProfile
	seen := make(map[string]struct{}, len(clients))
	for _, name := range clients {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		if _, skipped := s.clientSkip[normalized]; skipped {
			continue
		}
		seen[normalized] = struct{}{}
		if p, ok := s.registry.Get(normalized); ok {
			profiles = append(profiles, p)
		}
	}

	// If overrides were provided but all invalid, fall back to defaults.
	if len(profiles) == 0 && len(s.clientOrder) > 0 {
		defaults := []string{"android_vr", "web", "web_safari", "android", "ios", "mweb", "web_embedded", "tv"}
		for _, name := range defaults {
			if _, skipped := s.clientSkip[name]; skipped {
				continue
			}
			if p, ok := s.registry.Get(name); ok {
				profiles = append(profiles, p)
			}
		}
	}

	return profiles
}
