package client

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultHTTPClient_WithProxyURL(t *testing.T) {
	httpClient := defaultHTTPClient("http://127.0.0.1:3128", "", false)
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

func TestDefaultHTTPClient_InvalidProxyFailsLoudNoDirectLeak(t *testing.T) {
	// A requested proxy that cannot be parsed must NOT fall back to a direct
	// connection (which would leak the caller's real IP). Every request through
	// the returned client must fail instead.
	for _, proxyURL := range []string{
		"://bad-url",
		// Opaque form (no "//"): the exact mistake that silently downloaded
		// direct. url.Parse leaves Host empty here.
		"socks5:user:pass@host:50017",
	} {
		httpClient := defaultHTTPClient(proxyURL, "", false)
		if httpClient == http.DefaultClient {
			t.Fatalf("%q: must not return shared http.DefaultClient", proxyURL)
		}
		transport, ok := httpClient.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("%q: transport type = %T, want *http.Transport", proxyURL, httpClient.Transport)
		}
		req, err := http.NewRequest(http.MethodGet, "https://www.youtube.com/", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if _, err := transport.Proxy(req); err == nil {
			t.Fatalf("%q: proxy func returned nil error; expected a loud failure instead of a direct connection", proxyURL)
		}
	}
}

func TestValidateProxyURL(t *testing.T) {
	valid := []string{
		"",
		"http://127.0.0.1:3128",
		"socks5://host:1080",
		"socks5://user:pass@host:1080",
		// Password with reserved chars, correctly percent-encoded (% -> %25).
		"socks5://user:P5k%25tV@host:50017",
	}
	for _, in := range valid {
		if err := ValidateProxyURL(in); err != nil {
			t.Errorf("ValidateProxyURL(%q) = %v, want nil", in, err)
		}
	}
	invalid := []string{
		"://bad-url",
		// Opaque form: no "//", parses to empty Host.
		"socks5:host:1080",
		"socks5:user:pass@host:50017",
		// Raw "%tV" is an invalid percent-escape.
		"socks5://user:P5k%tV@host:50017",
	}
	for _, in := range invalid {
		if err := ValidateProxyURL(in); err == nil {
			t.Errorf("ValidateProxyURL(%q) = nil, want error", in)
		}
	}
}

func TestDefaultHTTPClient_WithSourceAddress(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp4 loopback unavailable: %v", err)
	}
	defer listener.Close()

	remoteAddrCh := make(chan string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteAddrCh <- r.RemoteAddr
		fmt.Fprint(w, "ok")
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	httpClient := defaultHTTPClient("", "127.0.0.1", false)
	resp, err := httpClient.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	resp.Body.Close()

	remoteAddr := <-remoteAddrCh
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", remoteAddr, err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("remote host=%q, want 127.0.0.1", host)
	}
}

func TestDefaultHTTPClient_InvalidSourceAddressDoesNotPanic(t *testing.T) {
	httpClient := defaultHTTPClient("", "not-an-ip", false)
	if httpClient == nil {
		t.Fatalf("defaultHTTPClient() returned nil")
	}
}

func TestDefaultHTTPClient_InsecureSkipVerify(t *testing.T) {
	httpClient := defaultHTTPClient("", "", true)
	if httpClient == nil {
		t.Fatalf("defaultHTTPClient() returned nil")
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", httpClient.Transport)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify=false, want true")
	}
}

func TestDefaultHTTPClient_DefaultReturnsOwnedTransport(t *testing.T) {
	// With no customization the client must still own its transport (a clone
	// of http.DefaultTransport), never sharing the global default transport
	// directly. Otherwise a later Jar assignment or Transport mutation leaks
	// into shared global state.
	httpClient := defaultHTTPClient("", "", false)
	if httpClient == nil {
		t.Fatalf("defaultHTTPClient() returned nil")
	}
	if httpClient.Transport == http.DefaultTransport {
		t.Fatalf("default client shares http.DefaultTransport; expected an owned clone")
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", httpClient.Transport)
	}
	if transport == http.DefaultTransport.(*http.Transport) {
		t.Fatalf("transport is the same pointer as http.DefaultTransport; expected an owned clone")
	}
}
