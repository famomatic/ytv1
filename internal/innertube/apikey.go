package innertube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

var innertubeAPIKeyPattern = regexp.MustCompile(`(?i)["']INNERTUBE_API_KEY["']\s*:\s*["']([^"']+)["']`)

type APIKeyResolver struct {
	httpClient *http.Client
	mu         sync.RWMutex
	cache      map[string]string
}

func NewAPIKeyResolver(httpClient *http.Client) *APIKeyResolver {
	return &APIKeyResolver{
		httpClient: httpClient,
		cache:      make(map[string]string),
	}
}

func (r *APIKeyResolver) Resolve(ctx context.Context, profile ClientProfile) (string, error) {
	fallback := strings.TrimSpace(profile.APIKey)
	if fallback == "" {
		fallback = defaultInnertubeAPIKey
	}
	if r == nil || r.httpClient == nil {
		return fallback, nil
	}

	host := strings.TrimSpace(profile.Host)
	if host == "" {
		return fallback, nil
	}
	cacheKey := strings.ToLower(host)

	if key, ok := r.get(cacheKey); ok {
		return key, nil
	}

	resolved, err := r.fetchFromWatch(ctx, host, profile.UserAgent)
	if err != nil || resolved == "" {
		r.set(cacheKey, fallback)
		if err != nil {
			return fallback, err
		}
		return fallback, nil
	}

	r.set(cacheKey, resolved)
	return resolved, nil
}

func (r *APIKeyResolver) get(host string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, ok := r.cache[host]
	return key, ok
}

func (r *APIKeyResolver) set(host, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[host] = key
}

func (r *APIKeyResolver) fetchFromWatch(ctx context.Context, host, userAgent string) (string, error) {
	watchURL := "https://" + host + "/watch?v=jNQXAC9IVRw"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, watchURL, nil)
	if err != nil {
		return "", err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("watch request failed: status=%d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	match := innertubeAPIKeyPattern.FindSubmatch(body)
	if len(match) < 2 {
		return "", fmt.Errorf("INNERTUBE_API_KEY not found in watch page")
	}
	return string(match[1]), nil
}
