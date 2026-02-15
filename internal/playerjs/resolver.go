package playerjs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Variant string

const (
    VariantMain Variant = "main"
    VariantTV   Variant = "tv"
)

type Resolver interface {
	GetPlayerJS(ctx context.Context, playerID string) (string, error)
}

type defaultResolver struct {
	client  *http.Client
	cache   Cache
	config  ResolverConfig
}

// ResolverConfig contains externally tunable settings for player JS fetches.
type ResolverConfig struct {
	BaseURL  string
	UserAgent string
	Headers  http.Header
}

const defaultPlayerJSUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

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
	if body, ok := r.cache.Get(playerURL); ok {
		return body, nil
	}

	baseURL := r.config.BaseURL
	if baseURL == "" {
		baseURL = "https://www.youtube.com"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+playerURL, nil)
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

	body := string(bodyBytes)
	r.cache.Set(playerURL, body)

	return body, nil
}

// Decipher logic will go into a separate file or struct, but Resolver fetches the code.
