package playerjs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetPlayerJS_NormalizesLocaleAndCachesByPlayerVariant(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/s/player/1798f86c/player_es6.vflset/en_US/base.js" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("ok-js"))
	}))
	defer srv.Close()

	resolver := NewResolver(http.DefaultClient, NewMemoryCache(), ResolverConfig{
		BaseURL:         srv.URL,
		PreferredLocale: "en_US",
	})
	ctx := context.Background()

	got1, err := resolver.GetPlayerJS(ctx, "/s/player/1798f86c/player_es6.vflset/ko_KR/base.js")
	if err != nil {
		t.Fatalf("GetPlayerJS() first call error = %v", err)
	}
	if got1 != "ok-js" {
		t.Fatalf("GetPlayerJS() first call body = %q, want %q", got1, "ok-js")
	}

	got2, err := resolver.GetPlayerJS(ctx, "/s/player/1798f86c/player_es6.vflset/ja_JP/base.js")
	if err != nil {
		t.Fatalf("GetPlayerJS() second call error = %v", err)
	}
	if got2 != "ok-js" {
		t.Fatalf("GetPlayerJS() second call body = %q, want %q", got2, "ok-js")
	}

	if requests != 1 {
		t.Fatalf("requests = %d, want %d", requests, 1)
	}
}

func TestGetPlayerJS_FallsBackToOriginalLocalePath(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/s/player/1798f86c/player_es6.vflset/ko_KR/base.js" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("ko-js"))
	}))
	defer srv.Close()

	resolver := NewResolver(http.DefaultClient, NewMemoryCache(), ResolverConfig{
		BaseURL:         srv.URL,
		PreferredLocale: "en_US",
	})
	ctx := context.Background()

	got, err := resolver.GetPlayerJS(ctx, "/s/player/1798f86c/player_es6.vflset/ko_KR/base.js")
	if err != nil {
		t.Fatalf("GetPlayerJS() error = %v", err)
	}
	if got != "ko-js" {
		t.Fatalf("GetPlayerJS() body = %q, want %q", got, "ko-js")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want %d (en_US try + original fallback)", requests, 2)
	}
}
