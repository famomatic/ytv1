package innertube

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CookieAuthContext struct {
	DelegatedSessionID string
	UserSessionID      string
	SessionIndex       *int
}

// ResolveVisitorData returns visitor data from request config or cookies.
func ResolveVisitorData(httpClient *http.Client, host string, configured string) string {
	if strings.TrimSpace(configured) != "" {
		return strings.TrimSpace(configured)
	}
	for _, c := range cookiesForHost(httpClient, host) {
		if strings.EqualFold(c.Name, "VISITOR_INFO1_LIVE") && strings.TrimSpace(c.Value) != "" {
			return strings.TrimSpace(c.Value)
		}
	}
	return ""
}

// BuildCookieAuthHeaders builds yt-dlp style auth headers from cookies.
func BuildCookieAuthHeaders(httpClient *http.Client, host string, now time.Time, ctx CookieAuthContext) http.Header {
	out := make(http.Header)
	if strings.TrimSpace(ctx.DelegatedSessionID) != "" {
		out.Set("X-Goog-PageId", strings.TrimSpace(ctx.DelegatedSessionID))
	}
	if strings.TrimSpace(ctx.DelegatedSessionID) != "" || ctx.SessionIndex != nil {
		authUser := 0
		if ctx.SessionIndex != nil {
			authUser = *ctx.SessionIndex
		}
		out.Set("X-Goog-AuthUser", strconv.Itoa(authUser))
	}

	if httpClient == nil || httpClient.Jar == nil {
		return out
	}
	cookies := cookiesForHost(httpClient, host)
	if len(cookies) == 0 {
		return out
	}

	cookieByName := make(map[string]string, len(cookies))
	for _, c := range cookies {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		cookieByName[name] = c.Value
	}

	origin := "https://" + host
	authValues := make([]string, 0, 3)
	appendAuth := func(scheme string, sid string) {
		sid = strings.TrimSpace(sid)
		if sid == "" {
			return
		}
		authValues = append(authValues, scheme+" "+sidHash(now.Unix(), sid, origin, strings.TrimSpace(ctx.UserSessionID)))
	}
	appendAuth("SAPISIDHASH", firstNonEmpty(cookieByName["SAPISID"], cookieByName["APISID"]))
	appendAuth("SAPISID1PHASH", cookieByName["__Secure-1PAPISID"])
	appendAuth("SAPISID3PHASH", cookieByName["__Secure-3PAPISID"])
	if len(authValues) > 0 {
		out.Set("Authorization", strings.Join(authValues, " "))
		out.Set("X-Origin", origin)
	}

	if strings.TrimSpace(cookieByName["LOGIN_INFO"]) != "" {
		out.Set("X-Youtube-Bootstrap-Logged-In", "true")
	}
	return out
}

func sidHash(ts int64, sid string, origin string, userSessionID string) string {
	hashParts := make([]string, 0, 4)
	if userSessionID != "" {
		hashParts = append(hashParts, userSessionID)
	}
	hashParts = append(hashParts, strconv.FormatInt(ts, 10), sid, origin)
	payload := strings.Join(hashParts, " ")
	sum := sha1.Sum([]byte(payload))
	parts := []string{strconv.FormatInt(ts, 10), hex.EncodeToString(sum[:])}
	if userSessionID != "" {
		parts = append(parts, "u")
	}
	return strings.Join(parts, "_")
}

func cookiesForHost(httpClient *http.Client, host string) []*http.Cookie {
	if httpClient == nil || httpClient.Jar == nil {
		return nil
	}
	u := &url.URL{Scheme: "https", Host: host}
	return httpClient.Jar.Cookies(u)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
