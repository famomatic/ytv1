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
}

func (c Config) ToInnerTubeConfig() innertube.Config {
    return innertube.Config{
        HTTPClient: c.HTTPClient,
        ProxyURL:   c.ProxyURL,
        PoTokenProvider: c.PoTokenProvider,
        VisitorData: c.VisitorData,
    }
}
