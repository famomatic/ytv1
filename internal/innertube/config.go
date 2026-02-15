package innertube

import (
	"context"
	"net/http"
	"time"
)

// PoTokenProvider defines an interface for injecting PO Tokens.
type PoTokenProvider interface {
	GetToken(ctx context.Context, clientID string) (string, error)
}

// Config holds configuration specific to InnerTube and Orchestrator.
type Config struct {
	HTTPClient                    *http.Client
	ProxyURL                      string
	PoTokenProvider               PoTokenProvider
	PoTokenFetchPolicy            map[VideoStreamingProtocol]PoTokenFetchPolicy
	VisitorData                   string
	PlayerJSBaseURL               string
	PlayerJSUserAgent             string
	PlayerJSHeaders               http.Header
	PlayerJSPreferredLocale       string
	ClientOverrides               []string
	ClientSkip                    []string
	RequestHeaders                http.Header
	RequestTimeout                time.Duration
	DisableFallbackClients        bool
	MetadataTransport             MetadataTransportConfig
	EnableDynamicAPIKeyResolution bool
}

type MetadataTransportConfig struct {
	MaxRetries       int
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	RetryStatusCodes []int
}
