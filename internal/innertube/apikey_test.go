package innertube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAPIKeyResolver_ResolvesFromWatchPage(t *testing.T) {
	var calls int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/watch" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`<script>ytcfg.set({"INNERTUBE_API_KEY":"dynamic_key_123"});</script>`))
	}))
	defer srv.Close()

	resolver := NewAPIKeyResolver(srv.Client())
	profile := WebClient
	profile.Host = strings.TrimPrefix(srv.URL, "https://")
	profile.APIKey = "fallback_key"

	got, err := resolver.Resolve(context.Background(), profile)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "dynamic_key_123" {
		t.Fatalf("Resolve() = %q, want %q", got, "dynamic_key_123")
	}

	got2, err := resolver.Resolve(context.Background(), profile)
	if err != nil {
		t.Fatalf("Resolve() second error = %v", err)
	}
	if got2 != "dynamic_key_123" {
		t.Fatalf("Resolve() second = %q, want %q", got2, "dynamic_key_123")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("watch page should be cached; calls=%d want=1", calls)
	}
}

func TestAPIKeyResolver_FallsBackWhenMissing(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>no key here</html>`))
	}))
	defer srv.Close()

	resolver := NewAPIKeyResolver(srv.Client())
	profile := WebClient
	profile.Host = strings.TrimPrefix(srv.URL, "https://")
	profile.APIKey = "fallback_key"

	got, err := resolver.Resolve(context.Background(), profile)
	if err == nil {
		t.Fatalf("expected extraction error, got nil")
	}
	if got != "fallback_key" {
		t.Fatalf("fallback key = %q, want %q", got, "fallback_key")
	}
}
