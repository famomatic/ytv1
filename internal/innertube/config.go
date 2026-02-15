package innertube

import (
	"context"
	"net/http"
)

// PoTokenProvider defines an interface for injecting PO Tokens.
type PoTokenProvider interface {
	GetToken(ctx context.Context, clientID string) (string, error)
}

// Config holds configuration specific to InnerTube and Orchestrator.
type Config struct {
	HTTPClient *http.Client
	ProxyURL   string
	PoTokenProvider PoTokenProvider
	VisitorData string
	PlayerJSBaseURL string
	PlayerJSUserAgent string
	PlayerJSHeaders http.Header
	ClientOverrides []string
}
