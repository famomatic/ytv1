package innertube

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestResolveVisitorDataPrefersConfiguredValue(t *testing.T) {
	client := &http.Client{}
	got := ResolveVisitorData(client, "www.youtube.com", "configured")
	if got != "configured" {
		t.Fatalf("visitor=%q, want configured", got)
	}
}

func TestResolveVisitorDataFromCookieJar(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	u, _ := url.Parse("https://www.youtube.com")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "VISITOR_INFO1_LIVE", Value: "visitor-cookie", Path: "/", Domain: ".youtube.com"},
	})
	client := &http.Client{Jar: jar}
	got := ResolveVisitorData(client, "www.youtube.com", "")
	if got != "visitor-cookie" {
		t.Fatalf("visitor=%q, want visitor-cookie", got)
	}
}

func TestBuildCookieAuthHeadersFromSapisidCookies(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	u, _ := url.Parse("https://www.youtube.com")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "SAPISID", Value: "sid-value", Path: "/", Domain: ".youtube.com"},
		{Name: "LOGIN_INFO", Value: "logged-in", Path: "/", Domain: ".youtube.com"},
	})
	client := &http.Client{Jar: jar}
	headers := BuildCookieAuthHeaders(client, "www.youtube.com", time.Unix(1700000000, 0), CookieAuthContext{})

	auth := headers.Get("Authorization")
	if !strings.HasPrefix(auth, "SAPISIDHASH 1700000000_") {
		t.Fatalf("authorization=%q, want SAPISIDHASH with timestamp prefix", auth)
	}
	if headers.Get("X-Origin") != "https://www.youtube.com" {
		t.Fatalf("x-origin=%q", headers.Get("X-Origin"))
	}
	if headers.Get("X-Youtube-Bootstrap-Logged-In") != "true" {
		t.Fatalf("expected bootstrap logged-in header")
	}
}

func TestBuildCookieAuthHeadersIncludesSessionHeaders(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	u, _ := url.Parse("https://www.youtube.com")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "SAPISID", Value: "sid-value", Path: "/", Domain: ".youtube.com"},
	})
	client := &http.Client{Jar: jar}
	sessionIndex := 2
	headers := BuildCookieAuthHeaders(client, "www.youtube.com", time.Unix(1700000000, 0), CookieAuthContext{
		DelegatedSessionID: "delegated-id",
		UserSessionID:      "user-session-id",
		SessionIndex:       &sessionIndex,
	})
	if headers.Get("X-Goog-PageId") != "delegated-id" {
		t.Fatalf("x-goog-pageid=%q", headers.Get("X-Goog-PageId"))
	}
	if headers.Get("X-Goog-AuthUser") != "2" {
		t.Fatalf("x-goog-authuser=%q", headers.Get("X-Goog-AuthUser"))
	}
	if !strings.Contains(headers.Get("Authorization"), "_u") {
		t.Fatalf("expected authorization suffix marker for user session id, got %q", headers.Get("Authorization"))
	}
}
