package client

import (
	"net/http"
	"net/url"
	"strings"
)

func defaultHTTPClient(proxyURL string) *http.Client {
	if strings.TrimSpace(proxyURL) == "" {
		return http.DefaultClient
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return http.DefaultClient
	}
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultClient
	}
	transport := baseTransport.Clone()
	transport.Proxy = http.ProxyURL(parsed)
	return &http.Client{Transport: transport}
}
