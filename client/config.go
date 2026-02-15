package client

import (
	"net/http"

	"github.com/mjmst/ytv1/internal/innertube"
)

// Config holds configuration for the YouTube client.
type Config struct {
	// HTTPClient is the client used for making requests.
	// If nil, http.DefaultClient is used.
	HTTPClient *http.Client

	// ProxyURL is the optional proxy URL to use for requests.
	// If HTTPClient is provided, this field is ignored.
	ProxyURL string

	// PoTokenProvider is the provider for PO Tokens.
	// If nil, PO Tokens will not be injected, which may cause throttling or errors.
	PoTokenProvider innertube.PoTokenProvider

	// VisitorData is the "VISITOR_INFO1_LIVE" cookie value.
	// Use this to persist sessions or emulate a specific user context.
	VisitorData string

	// PlayerJSBaseURL overrides player JS fetch host (default: https://www.youtube.com).
	PlayerJSBaseURL string

	// PlayerJSUserAgent overrides player JS fetch User-Agent.
	// If empty, package fallback is used.
	PlayerJSUserAgent string

	// PlayerJSHeaders are additional headers for player JS fetches.
	PlayerJSHeaders http.Header

	// PlayerJSPreferredLocale controls canonical locale for player JS fetch path.
	// Default is "en_US". Fetch falls back to the original watch-page locale path.
	PlayerJSPreferredLocale string

	// ClientOverrides sets Innertube client trial order (e.g. "web", "ios", "android").
	// If empty, package defaults are used.
	ClientOverrides []string

	// DisableDynamicAPIKeyResolution disables watch-page ytcfg API key extraction.
	// Default is false (dynamic resolution enabled).
	DisableDynamicAPIKeyResolution bool
}

func (c Config) ToInnerTubeConfig() innertube.Config {
	return innertube.Config{
		HTTPClient:                    c.HTTPClient,
		ProxyURL:                      c.ProxyURL,
		PoTokenProvider:               c.PoTokenProvider,
		VisitorData:                   c.VisitorData,
		PlayerJSBaseURL:               c.PlayerJSBaseURL,
		PlayerJSUserAgent:             c.PlayerJSUserAgent,
		PlayerJSHeaders:               c.PlayerJSHeaders,
		PlayerJSPreferredLocale:       c.PlayerJSPreferredLocale,
		ClientOverrides:               c.ClientOverrides,
		EnableDynamicAPIKeyResolution: !c.DisableDynamicAPIKeyResolution,
	}
}
