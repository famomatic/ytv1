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
	if proxyURL == "" && sourceAddress == "" && !insecureSkipVerify {
		// Return a dedicated client sharing the default transport rather than
		// http.DefaultClient, so a later caller mutating this client's Jar or
		// Transport does not poison the shared global default client.
		return &http.Client{Transport: http.DefaultTransport}
	}
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Transport: http.DefaultTransport}
	}
	transport := baseTransport.Clone()
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return http.DefaultClient
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
