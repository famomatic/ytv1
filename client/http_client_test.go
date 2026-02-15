package client

import (
	"net/http"
	"testing"
)

func TestDefaultHTTPClient_WithProxyURL(t *testing.T) {
	httpClient := defaultHTTPClient("http://127.0.0.1:3128")
	if httpClient == nil {
		t.Fatalf("defaultHTTPClient() returned nil")
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", httpClient.Transport)
	}
	req, err := http.NewRequest(http.MethodGet, "https://www.youtube.com/watch?v=jNQXAC9IVRw", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("proxy function error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:3128" {
		t.Fatalf("proxyURL = %v, want http://127.0.0.1:3128", proxyURL)
	}
}

func TestDefaultHTTPClient_InvalidProxyFallsBack(t *testing.T) {
	httpClient := defaultHTTPClient("://bad-url")
	if httpClient != http.DefaultClient {
		t.Fatalf("expected fallback to http.DefaultClient")
	}
}
