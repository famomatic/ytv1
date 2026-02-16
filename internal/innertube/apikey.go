package innertube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var innertubeAPIKeyPattern = regexp.MustCompile(`(?i)["']INNERTUBE_API_KEY["']\s*:\s*["']([^"']+)["']`)
var visitorDataPattern = regexp.MustCompile(`(?i)["']VISITOR_DATA["']\s*:\s*["']([^"']+)["']`)
var delegatedSessionIDPattern = regexp.MustCompile(`(?i)["']DELEGATED_SESSION_ID["']\s*:\s*["']([^"']+)["']`)
var userSessionIDPattern = regexp.MustCompile(`(?i)["']USER_SESSION_ID["']\s*:\s*["']([^"']+)["']`)
var dataSyncIDPattern = regexp.MustCompile(`(?i)["']DATASYNC_ID["']\s*:\s*["']([^"']+)["']`)
var sessionIndexPattern = regexp.MustCompile(`(?i)["']SESSION_INDEX["']\s*:\s*["']?(\d+)["']?`)
var signatureTimestampPattern = regexp.MustCompile(`(?i)["']STS["']\s*:\s*["']?(\d+)["']?`)
var playerSignatureTimestampPattern = regexp.MustCompile(`(?i)(?:signatureTimestamp|sts)\s*:\s*(\d{5})`)
var playerJSURLCfgPattern = regexp.MustCompile(`(?i)["']PLAYER_JS_URL["']\s*:\s*["']([^"']+)["']`)
var webPlayerContextJSURLPattern = regexp.MustCompile(`(?i)["']jsUrl["']\s*:\s*["']([^"']+/base\.js)["']`)
var playerURLPattern = regexp.MustCompile(`(/s/player/[A-Za-z0-9_-]+/[A-Za-z0-9._/-]*/base\.js)`)

type resolvedWatchData struct {
	APIKey             string
	VisitorData        string
	DelegatedSessionID string
	UserSessionID      string
	SessionIndex       *int
	SignatureTimestamp int
}

type APIKeyResolver struct {
	httpClient *http.Client
	mu         sync.RWMutex
	cache      map[string]resolvedWatchData
}

func NewAPIKeyResolver(httpClient *http.Client) *APIKeyResolver {
	return &APIKeyResolver{
		httpClient: httpClient,
		cache:      make(map[string]resolvedWatchData),
	}
}

func (r *APIKeyResolver) Resolve(ctx context.Context, profile ClientProfile, videoID string) (string, error) {
	fallback := strings.TrimSpace(profile.APIKey)
	if fallback == "" {
		fallback = defaultInnertubeAPIKey
	}
	if r == nil || r.httpClient == nil {
		return fallback, nil
	}

	cacheKey := profileCacheKey(profile)
	if cacheKey == "" {
		return fallback, nil
	}

	if data, ok := r.get(cacheKey); ok {
		if strings.TrimSpace(data.APIKey) == "" {
			return fallback, nil
		}
		return data.APIKey, nil
	}

	resolved, err := r.fetchFromWatch(ctx, profile, videoID)
	if err != nil || strings.TrimSpace(resolved.APIKey) == "" {
		r.set(cacheKey, resolvedWatchData{APIKey: fallback})
		if err != nil {
			return fallback, err
		}
		return fallback, nil
	}

	r.set(cacheKey, resolved)
	return resolved.APIKey, nil
}

func (r *APIKeyResolver) ResolveVisitorData(ctx context.Context, profile ClientProfile, videoID string) string {
	if r == nil || r.httpClient == nil {
		return ""
	}
	cacheKey := profileCacheKey(profile)
	if cacheKey == "" {
		return ""
	}
	if data, ok := r.get(cacheKey); ok {
		return strings.TrimSpace(data.VisitorData)
	}
	resolved, err := r.fetchFromWatch(ctx, profile, videoID)
	if err != nil {
		return ""
	}
	r.set(cacheKey, resolved)
	return strings.TrimSpace(resolved.VisitorData)
}

func (r *APIKeyResolver) ResolveCookieAuthContext(ctx context.Context, profile ClientProfile, videoID string) CookieAuthContext {
	if r == nil || r.httpClient == nil {
		return CookieAuthContext{}
	}
	cacheKey := profileCacheKey(profile)
	if cacheKey == "" {
		return CookieAuthContext{}
	}
	if data, ok := r.get(cacheKey); ok {
		return data.toCookieAuthContext()
	}
	resolved, err := r.fetchFromWatch(ctx, profile, videoID)
	if err != nil && resolved.APIKey == "" && resolved.VisitorData == "" {
		return CookieAuthContext{}
	}
	r.set(cacheKey, resolved)
	return resolved.toCookieAuthContext()
}

func (r *APIKeyResolver) ResolveSignatureTimestamp(ctx context.Context, profile ClientProfile, videoID string) int {
	if r == nil || r.httpClient == nil {
		return 0
	}
	cacheKey := profileCacheKey(profile)
	if cacheKey == "" {
		return 0
	}
	if data, ok := r.get(cacheKey); ok {
		return data.SignatureTimestamp
	}
	resolved, err := r.fetchFromWatch(ctx, profile, videoID)
	if err != nil && resolved.APIKey == "" && resolved.VisitorData == "" {
		return 0
	}
	r.set(cacheKey, resolved)
	return resolved.SignatureTimestamp
}

func (r *APIKeyResolver) get(host string) (resolvedWatchData, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, ok := r.cache[host]
	return key, ok
}

func (r *APIKeyResolver) set(host string, key resolvedWatchData) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[host] = key
}

func (r *APIKeyResolver) fetchFromWatch(ctx context.Context, profile ClientProfile, videoID string) (resolvedWatchData, error) {
	watchURL := watchPageURLForProfile(profile, videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, watchURL, nil)
	if err != nil {
		return resolvedWatchData{}, err
	}
	if profile.UserAgent != "" {
		req.Header.Set("User-Agent", profile.UserAgent)
	}
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return resolvedWatchData{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resolvedWatchData{}, fmt.Errorf("watch request failed: status=%d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resolvedWatchData{}, err
	}

	resolved := resolvedWatchData{}
	match := innertubeAPIKeyPattern.FindSubmatch(body)
	if len(match) >= 2 {
		resolved.APIKey = strings.TrimSpace(string(match[1]))
	}
	visitorMatch := visitorDataPattern.FindSubmatch(body)
	if len(visitorMatch) >= 2 {
		resolved.VisitorData = strings.TrimSpace(string(visitorMatch[1]))
	}
	delegatedMatch := delegatedSessionIDPattern.FindSubmatch(body)
	if len(delegatedMatch) >= 2 {
		resolved.DelegatedSessionID = strings.TrimSpace(string(delegatedMatch[1]))
	}
	userMatch := userSessionIDPattern.FindSubmatch(body)
	if len(userMatch) >= 2 {
		resolved.UserSessionID = strings.TrimSpace(string(userMatch[1]))
	}
	dataSyncMatch := dataSyncIDPattern.FindSubmatch(body)
	if len(dataSyncMatch) >= 2 {
		delegated, user := parseDataSyncID(strings.TrimSpace(string(dataSyncMatch[1])))
		if resolved.DelegatedSessionID == "" {
			resolved.DelegatedSessionID = delegated
		}
		if resolved.UserSessionID == "" {
			resolved.UserSessionID = user
		}
	}
	sessionIndexMatch := sessionIndexPattern.FindSubmatch(body)
	if len(sessionIndexMatch) >= 2 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(string(sessionIndexMatch[1]))); err == nil {
			resolved.SessionIndex = &parsed
		}
	}
	stsMatch := signatureTimestampPattern.FindSubmatch(body)
	if len(stsMatch) >= 2 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(string(stsMatch[1]))); err == nil {
			resolved.SignatureTimestamp = parsed
		}
	}
	if resolved.SignatureTimestamp == 0 {
		if playerURL := extractPlayerURLFromWatchBody(body); playerURL != "" {
			if sts, err := r.extractSignatureTimestampFromPlayerJS(ctx, profile, playerURL); err == nil {
				resolved.SignatureTimestamp = sts
			}
		}
	}
	if resolved.APIKey == "" {
		return resolved, fmt.Errorf("INNERTUBE_API_KEY not found in watch page")
	}
	return resolved, nil
}

func extractPlayerURLFromWatchBody(body []byte) string {
	for _, re := range []*regexp.Regexp{playerJSURLCfgPattern, webPlayerContextJSURLPattern, playerURLPattern} {
		match := re.FindSubmatch(body)
		if len(match) < 2 {
			continue
		}
		candidate := strings.TrimSpace(string(match[1]))
		if candidate == "" {
			continue
		}
		candidate = strings.ReplaceAll(candidate, `\/`, "/")
		if strings.HasPrefix(candidate, "//") {
			return "https:" + candidate
		}
		return candidate
	}
	return ""
}

func (r *APIKeyResolver) extractSignatureTimestampFromPlayerJS(ctx context.Context, profile ClientProfile, playerURL string) (int, error) {
	fullURL := playerURL
	if !strings.HasPrefix(fullURL, "http://") && !strings.HasPrefix(fullURL, "https://") {
		host := strings.TrimSpace(profile.Host)
		if host == "" {
			host = "www.youtube.com"
		}
		fullURL = "https://" + host + playerURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return 0, err
	}
	if ua := strings.TrimSpace(profile.UserAgent); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("player js request failed: status=%d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	match := playerSignatureTimestampPattern.FindSubmatch(body)
	if len(match) < 2 {
		return 0, fmt.Errorf("signatureTimestamp not found in player js")
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(string(bytes.TrimSpace(match[1]))))
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func parseDataSyncID(dataSyncID string) (string, string) {
	if strings.TrimSpace(dataSyncID) == "" {
		return "", ""
	}
	parts := strings.SplitN(dataSyncID, "||", 2)
	if len(parts) == 2 {
		first := strings.TrimSpace(parts[0])
		second := strings.TrimSpace(parts[1])
		if second != "" {
			return first, second
		}
		return "", first
	}
	return "", strings.TrimSpace(parts[0])
}

func (d resolvedWatchData) toCookieAuthContext() CookieAuthContext {
	return CookieAuthContext{
		DelegatedSessionID: strings.TrimSpace(d.DelegatedSessionID),
		UserSessionID:      strings.TrimSpace(d.UserSessionID),
		SessionIndex:       d.SessionIndex,
	}
}

func profileCacheKey(profile ClientProfile) string {
	host := strings.ToLower(strings.TrimSpace(profile.Host))
	if host == "" {
		return ""
	}
	id := strings.ToLower(strings.TrimSpace(profile.ID))
	if id == "" {
		id = strings.ToLower(strings.TrimSpace(profile.Name))
	}
	return host + "|" + id
}

func watchPageURLForProfile(profile ClientProfile, videoID string) string {
	id := strings.ToLower(strings.TrimSpace(profile.ID))
	videoID = strings.TrimSpace(videoID)
	switch {
	case id == "mweb":
		if videoID == "" {
			return "https://m.youtube.com"
		}
		return "https://m.youtube.com/watch?v=" + videoID
	case strings.HasPrefix(id, "web_embedded"):
		if videoID == "" {
			return "https://www.youtube.com/embed/"
		}
		return "https://www.youtube.com/embed/" + videoID + "?html5=1"
	case strings.HasPrefix(id, "tv"):
		return "https://www.youtube.com/tv"
	default:
		host := strings.TrimSpace(profile.Host)
		if host == "" {
			host = "www.youtube.com"
		}
		if videoID == "" {
			return "https://" + host
		}
		return "https://" + host + "/watch?v=" + videoID
	}
}
