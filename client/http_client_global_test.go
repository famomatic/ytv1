package client

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
)

// TestNewClient_DoesNotMutateSharedHTTPClient ensures that when a caller
// passes a shared *http.Client and a cookie jar, NewClient does not mutate
// the caller's client (e.g. setting its Jar), which would leak cookie state
// across unrelated clients sharing that *http.Client.
func TestNewClient_DoesNotMutateSharedHTTPClient(t *testing.T) {
	shared := &http.Client{}
	if shared.Jar != nil {
		t.Fatalf("precondition: shared client jar should be nil")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}

	_ = New(Config{
		HTTPClient: shared,
		CookieJar:  jar,
	})

	if shared.Jar != nil {
		t.Fatalf("NewClient mutated the shared *http.Client's Jar; it should remain nil")
	}
}

// TestNewClient_DoesNotPoisonDefaultClient ensures the default (no-config)
// HTTP client returned by NewClient is distinct from http.DefaultClient, so
// installing a cookie jar does not modify the process-wide default client.
func TestNewClient_DoesNotPoisonDefaultClient(t *testing.T) {
	if http.DefaultClient.Jar != nil {
		t.Skipf("http.DefaultClient already has a jar before this test; cannot assert cleanly")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	c := New(Config{
		CookieJar: jar,
	})
	if c.HTTPClient() == http.DefaultClient {
		t.Fatalf("NewClient returned the shared http.DefaultClient; jar assignment would poison global state")
	}
	if http.DefaultClient.Jar != nil {
		t.Fatalf("http.DefaultClient.Jar was modified by NewClient")
	}
}
