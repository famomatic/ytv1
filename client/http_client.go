package client

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func defaultHTTPClient(proxyURL, sourceAddress string, insecureSkipVerify bool) *http.Client {
	proxyURL = strings.TrimSpace(proxyURL)
	sourceAddress = strings.TrimSpace(sourceAddress)

	// Always return a client whose transport is owned by this client. Even in
	// the no-customization case we clone the default transport so that a later
	// assignment of Jar or mutation of Transport settings never leaks back
	// into the shared http.DefaultTransport / http.DefaultClient. Returning
	// http.DefaultClient (or http.DefaultTransport directly) would let cookie
	// jars and dialer overrides poison global state across all ytv1 clients
	// and any other code sharing the default transport.
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Non-standard default transport (e.g. test stubs): wrap a dedicated
		// client around it but still avoid returning http.DefaultClient so
		// Jar assignment stays local.
		return &http.Client{Transport: http.DefaultTransport}
	}
	transport := baseTransport.Clone()
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			// Invalid proxy URL: fall back to a safe owned client instead of
			// http.DefaultClient, whose Jar/Transport are shared global state.
			return &http.Client{Transport: transport}
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	if ip := net.ParseIP(sourceAddress); ip != nil {
		dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: ip}}
		transport.DialContext = dialer.DialContext
	}
	if insecureSkipVerify {
		tlsConfig := transport.TLSClientConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{}
		} else {
			tlsConfig = tlsConfig.Clone()
		}
		tlsConfig.InsecureSkipVerify = true
		transport.TLSClientConfig = tlsConfig
	}
	return &http.Client{Transport: transport}
}
