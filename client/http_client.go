package client

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ValidateProxyURL reports whether proxyURL is a usable proxy URL. An empty
// string is valid and means "no proxy". A non-empty value must parse to a URL
// with both a scheme and a host.
//
// The two mistakes this catches are the reason it exists:
//   - "socks5:host:port" (no "//") parses as an opaque URL with an empty Host,
//     so http.Transport would silently ignore it and connect directly.
//   - a password containing raw reserved characters (%, ^, @, ...) makes
//     url.Parse reject the userinfo; those characters must be percent-encoded.
//
// Callers must treat a non-nil error as fatal for the request: a requested
// proxy that cannot be honored must never fall back to a direct connection,
// which would leak the caller's real IP to the target.
func ValidateProxyURL(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL %q: %w (percent-encode reserved characters in the password, e.g. %% as %%25)", proxyURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid proxy URL %q: expected scheme://[user:pass@]host:port (use \"//\" after the scheme, e.g. socks5://user:pass@host:port)", proxyURL)
	}
	return nil
}

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
	// Backstop for a non-responsive server: bound the wait for response headers
	// so a media download that runs without an overall deadline cannot hang
	// forever on a dead connection. This limits only the header wait, never
	// body-transfer time, so large/long downloads are unaffected.
	if transport.ResponseHeaderTimeout == 0 {
		transport.ResponseHeaderTimeout = 60 * time.Second
	}
	if proxyURL != "" {
		if err := ValidateProxyURL(proxyURL); err != nil {
			// A proxy was requested but cannot be honored. Do NOT fall back to a
			// direct connection: after the user explicitly asked to tunnel,
			// connecting directly would silently leak the client's real IP to
			// the target. Instead install a Proxy func that fails every request
			// through this transport, so the error surfaces loudly at request
			// time rather than deanonymizing the user. (The CLI validates the
			// proxy earlier and aborts before any request; this is the
			// library-level backstop for callers of client.New.)
			transport.Proxy = func(*http.Request) (*url.URL, error) { return nil, err }
			return &http.Client{Transport: transport}
		}
		parsed, _ := url.Parse(proxyURL)
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
