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
		_, _ = w.Write([]byte(`<script>ytcfg.set({"INNERTUBE_API_KEY":"dynamic_key_123","VISITOR_DATA":"visitor_123","SESSION_INDEX":3,"DELEGATED_SESSION_ID":"delegated_abc","USER_SESSION_ID":"user_xyz","STS":20542});</script>`))
	}))
	defer srv.Close()

	resolver := NewAPIKeyResolver(srv.Client())
	profile := WebClient
	profile.Host = strings.TrimPrefix(srv.URL, "https://")
	profile.APIKey = "fallback_key"

	got, err := resolver.Resolve(context.Background(), profile, "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "dynamic_key_123" {
		t.Fatalf("Resolve() = %q, want %q", got, "dynamic_key_123")
	}

	got2, err := resolver.Resolve(context.Background(), profile, "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("Resolve() second error = %v", err)
	}
	if got2 != "dynamic_key_123" {
		t.Fatalf("Resolve() second = %q, want %q", got2, "dynamic_key_123")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("watch page should be cached; calls=%d want=1", calls)
	}

	visitor := resolver.ResolveVisitorData(context.Background(), profile, "jNQXAC9IVRw")
	if visitor != "visitor_123" {
		t.Fatalf("ResolveVisitorData() = %q, want %q", visitor, "visitor_123")
	}
	authCtx := resolver.ResolveCookieAuthContext(context.Background(), profile, "jNQXAC9IVRw")
	if authCtx.DelegatedSessionID != "delegated_abc" {
		t.Fatalf("delegated session = %q", authCtx.DelegatedSessionID)
	}
	if authCtx.UserSessionID != "user_xyz" {
		t.Fatalf("user session = %q", authCtx.UserSessionID)
	}
	if authCtx.SessionIndex == nil || *authCtx.SessionIndex != 3 {
		t.Fatalf("session index = %+v", authCtx.SessionIndex)
	}
	if sts := resolver.ResolveSignatureTimestamp(context.Background(), profile, "jNQXAC9IVRw"); sts != 20542 {
		t.Fatalf("ResolveSignatureTimestamp()=%d, want 20542", sts)
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

	got, err := resolver.Resolve(context.Background(), profile, "jNQXAC9IVRw")
	if err == nil {
		t.Fatalf("expected extraction error, got nil")
	}
	if got != "fallback_key" {
		t.Fatalf("fallback key = %q, want %q", got, "fallback_key")
	}
}

func TestWatchPageURLForProfile(t *testing.T) {
	web := WebClient
	if got := watchPageURLForProfile(web, "abc123xyz00"); got != "https://www.youtube.com/watch?v=abc123xyz00" {
		t.Fatalf("web url=%q", got)
	}
	mweb := MWebClient
	if got := watchPageURLForProfile(mweb, "abc123xyz00"); got != "https://m.youtube.com/watch?v=abc123xyz00" {
		t.Fatalf("mweb url=%q", got)
	}
	embedded := WebEmbeddedClient
	if got := watchPageURLForProfile(embedded, "abc123xyz00"); got != "https://www.youtube.com/embed/abc123xyz00?html5=1" {
		t.Fatalf("embedded url=%q", got)
	}
	tv := TVClient
	if got := watchPageURLForProfile(tv, "abc123xyz00"); got != "https://www.youtube.com/tv" {
		t.Fatalf("tv url=%q", got)
	}
}

func TestParseDataSyncID(t *testing.T) {
	delegated, user := parseDataSyncID("delegated||user")
	if delegated != "delegated" || user != "user" {
		t.Fatalf("unexpected parse delegated=%q user=%q", delegated, user)
	}
	delegated, user = parseDataSyncID("user_only||")
	if delegated != "" || user != "user_only" {
		t.Fatalf("unexpected primary parse delegated=%q user=%q", delegated, user)
	}
}

func TestAPIKeyResolver_ResolveSignatureTimestampFromPlayerJSFallback(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/watch":
			_, _ = w.Write([]byte(`<script>ytcfg.set({"INNERTUBE_API_KEY":"dynamic_key_123","PLAYER_JS_URL":"\/s\/player\/abcd1234\/player_ias.vflset\/en_US\/base.js"});</script>`))
		case "/s/player/abcd1234/player_ias.vflset/en_US/base.js":
			_, _ = w.Write([]byte(`var cfg = {signatureTimestamp: 20494};`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	resolver := NewAPIKeyResolver(srv.Client())
	profile := WebClient
	profile.Host = strings.TrimPrefix(srv.URL, "https://")
	profile.APIKey = "fallback_key"

	if sts := resolver.ResolveSignatureTimestamp(context.Background(), profile, "jNQXAC9IVRw"); sts != 20494 {
		t.Fatalf("ResolveSignatureTimestamp()=%d, want 20494", sts)
	}
}
