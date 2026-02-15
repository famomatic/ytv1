package playerjs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type Variant string

const (
	VariantMain Variant = "main"
	VariantTV   Variant = "tv"
)

type Resolver interface {
	GetPlayerJS(ctx context.Context, playerID string) (string, error)
	GetPlayerURL(ctx context.Context, videoID string) (string, error)
}

type defaultResolver struct {
	client *http.Client
	cache  Cache
	config ResolverConfig
}

// ResolverConfig contains externally tunable settings for player JS fetches.
type ResolverConfig struct {
	BaseURL         string
	UserAgent       string
	Headers         http.Header
	PreferredLocale string
}

const defaultPlayerJSUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
const defaultPlayerJSLocale = "en_US"

var playerURLPattern = regexp.MustCompile(`(/s/player/[A-Za-z0-9_-]+/[A-Za-z0-9._/-]*/base\.js)`)
var playerPathPattern = regexp.MustCompile(`^/s/player/([A-Za-z0-9_-]+)/(.+)$`)
var localePathPattern = regexp.MustCompile(`(?i)(player(?:_[a-z0-9]+)?\.vflset)/[a-z]{2,3}_[a-z]{2,3}/base\.js$`)
var nonAlnumPattern = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func NewResolver(client *http.Client, cache Cache, cfg ...ResolverConfig) Resolver {
	resolverConfig := ResolverConfig{}
	if len(cfg) > 0 {
		resolverConfig = cfg[0]
	}
	return &defaultResolver{
		client: client,
		cache:  cache,
		config: resolverConfig,
	}
}

// Regex to extract player ID from URL if needed, but usually we get the URL from the Innertube response.
// For now, let's assume we get the full URL.

func (r *defaultResolver) GetPlayerJS(ctx context.Context, playerURL string) (string, error) {
	normalizedPath := r.normalizePlayerPath(playerURL)
	cacheKey := r.playerCacheKey(normalizedPath)
	if body, ok := r.cache.Get(cacheKey); ok {
		return body, nil
	}

	candidates := []string{normalizedPath}
	if playerURL != normalizedPath {
		candidates = append(candidates, playerURL)
	}

	var lastErr error
	for _, candidate := range candidates {
		body, err := r.fetchPlayerJS(ctx, candidate)
		if err != nil {
			lastErr = err
			continue
		}
		r.cache.Set(cacheKey, body)
		return body, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("failed to fetch player JS")
}

func (r *defaultResolver) fetchPlayerJS(ctx context.Context, playerURL string) (string, error) {
	urlToFetch := playerURL
	if !strings.HasPrefix(urlToFetch, "http://") && !strings.HasPrefix(urlToFetch, "https://") {
		baseURL := r.config.BaseURL
		if baseURL == "" {
			baseURL = "https://www.youtube.com"
		}
		urlToFetch = strings.TrimRight(baseURL, "/") + playerURL
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlToFetch, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	ua := r.config.UserAgent
	if ua == "" {
		ua = defaultPlayerJSUserAgent
	}
	req.Header.Set("User-Agent", ua)
	for k, values := range r.config.Headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch player JS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read body: %w", err)
	}

	return string(bodyBytes), nil
}

func (r *defaultResolver) GetPlayerURL(ctx context.Context, videoID string) (string, error) {
	baseURL := r.config.BaseURL
	if baseURL == "" {
		baseURL = "https://www.youtube.com"
	}

	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/watch")
	if err != nil {
		return "", fmt.Errorf("failed to build watch url: %w", err)
	}
	q := u.Query()
	q.Set("v", videoID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	ua := r.config.UserAgent
	if ua == "" {
		ua = defaultPlayerJSUserAgent
	}
	req.Header.Set("User-Agent", ua)
	for k, values := range r.config.Headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch watch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read body: %w", err)
	}

	m := playerURLPattern.FindSubmatch(body)
	if len(m) < 2 {
		return "", fmt.Errorf("player url not found")
	}
	return string(m[1]), nil
}

func (r *defaultResolver) normalizePlayerPath(playerURL string) string {
	u, err := url.Parse(playerURL)
	if err == nil && u.Path != "" {
		playerURL = u.Path
	}
	locale := r.config.PreferredLocale
	if locale == "" {
		locale = defaultPlayerJSLocale
	}
	if localePathPattern.MatchString(playerURL) {
		return localePathPattern.ReplaceAllString(playerURL, "${1}/"+locale+"/base.js")
	}
	return playerURL
}

func (r *defaultResolver) playerCacheKey(playerPath string) string {
	m := playerPathPattern.FindStringSubmatch(playerPath)
	if len(m) < 3 {
		return playerPath
	}
	playerID := m[1]
	variant := nonAlnumPattern.ReplaceAllString(m[2], "_")
	return playerID + ":" + variant
}
