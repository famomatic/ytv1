package innertube

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/famomatic/ytv1/internal/iox"
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

// watchDataCacheTTL bounds how long resolved watch-page data (API key,
// visitor data, signature timestamp, session ids) is served before a
// re-fetch. YouTube rotates player JS / STS every few hours; without a TTL a
// stale SignatureTimestamp/VisitorData would be served for the whole process
// lifetime (the B147 session-cache staleness class). Mirrors the 6h player-JS
// cache TTL.
const watchDataCacheTTL = 6 * time.Hour

type resolvedWatchData struct {
	APIKey             string
	VisitorData        string
	DelegatedSessionID string
	UserSessionID      string
	SessionIndex       *int
	SignatureTimestamp int
	createdAt          time.Time
}

type APIKeyResolver struct {
	httpClient *http.Client
	mu         sync.RWMutex
	cache      map[string]resolvedWatchData
	fetchLocks *keyLock
}

func NewAPIKeyResolver(httpClient *http.Client) *APIKeyResolver {
	return &APIKeyResolver{
		httpClient: httpClient,
		cache:      make(map[string]resolvedWatchData),
		fetchLocks: newKeyLock(),
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

	data, err := r.resolveWatchDataCached(ctx, cacheKey, profile, videoID)
	if err != nil {
		// Do NOT cache the fallback on error: with a TTL'd cache a permanent
		// poison entry would (a) never expire fast enough and (b) make every
		// later ResolveVisitorData/ResolveSignatureTimestamp/Resolve return
		// ""/0/fallback without retrying. The per-key fetch lock already
		// prevents a thundering herd, so leaving the entry absent lets the
		// next caller retry cleanly.
		return fallback, err
	}
	if strings.TrimSpace(data.APIKey) == "" {
		return fallback, nil
	}
	return data.APIKey, nil
}

func (r *APIKeyResolver) ResolveVisitorData(ctx context.Context, profile ClientProfile, videoID string) string {
	if r == nil || r.httpClient == nil {
		return ""
	}
	cacheKey := profileCacheKey(profile)
	if cacheKey == "" {
		return ""
	}
	data, err := r.resolveWatchDataCached(ctx, cacheKey, profile, videoID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(data.VisitorData)
}

func (r *APIKeyResolver) ResolveCookieAuthContext(ctx context.Context, profile ClientProfile, videoID string) CookieAuthContext {
	if r == nil || r.httpClient == nil {
		return CookieAuthContext{}
	}
	cacheKey := profileCacheKey(profile)
	if cacheKey == "" {
		return CookieAuthContext{}
	}
	data, err := r.resolveWatchDataCached(ctx, cacheKey, profile, videoID)
	if err != nil {
		return CookieAuthContext{}
	}
	return data.toCookieAuthContext()
}

func (r *APIKeyResolver) ResolveSignatureTimestamp(ctx context.Context, profile ClientProfile, videoID string) int {
	if r == nil || r.httpClient == nil {
		return 0
	}
	cacheKey := profileCacheKey(profile)
	if cacheKey == "" {
		return 0
	}
	data, err := r.resolveWatchDataCached(ctx, cacheKey, profile, videoID)
	if err != nil {
		return 0
	}
	return data.SignatureTimestamp
}

// resolveWatchDataCached returns the cached resolved data for the given
// profile cache key, fetching from the watch page on miss. Concurrent
// callers for the same key share a single fetch (per-key serialization)
// to avoid thundering-herd watch-page requests. On a successful fetch the
// result is merged into the cache; on error the cache is left untouched so
// the next caller can retry. The fetch error is propagated to the caller.
func (r *APIKeyResolver) resolveWatchDataCached(ctx context.Context, cacheKey string, profile ClientProfile, videoID string) (resolvedWatchData, error) {
	if data, ok := r.get(cacheKey); ok {
		return data, nil
	}

	lock, release := r.fetchLocks.acquire(cacheKey)
	defer release()
	if err := lock.LockContext(ctx); err != nil {
		return resolvedWatchData{}, err
	}
	defer lock.Unlock()

	// Re-check under the lock: a concurrent caller may have populated it.
	if data, ok := r.get(cacheKey); ok {
		return data, nil
	}

	resolved, err := r.fetchFromWatch(ctx, profile, videoID)
	if err == nil {
		r.set(cacheKey, resolved)
		return resolved, nil
	}
	// A successful fetch (err == nil) with an empty payload is still a
	// cacheable miss for individual fields; only hard fetch errors are
	// propagated so callers can surface them as before.
	return resolved, err
}
func (r *APIKeyResolver) get(host string) (resolvedWatchData, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, ok := r.cache[host]
	if !ok {
		return resolvedWatchData{}, false
	}
	// Expire stale entries so rotated STS/visitor data is re-fetched instead
	// of being served indefinitely.
	if !key.createdAt.IsZero() && time.Since(key.createdAt) > watchDataCacheTTL {
		return resolvedWatchData{}, false
	}
	return key, true
}

func (r *APIKeyResolver) set(host string, key resolvedWatchData) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Merge instead of overwrite so concurrent resolves for different
	// fields (APIKey vs VisitorData vs STS) of the same profile do not
	// clobber each other. Non-empty values in the incoming entry win.
	existing, ok := r.cache[host]
	// Treat an expired entry as absent so a fresh fetch resets the TTL clock
	// rather than inheriting the old (expired) createdAt via merge.
	if ok && !existing.createdAt.IsZero() && time.Since(existing.createdAt) > watchDataCacheTTL {
		ok = false
	}
	if !ok {
		if key.createdAt.IsZero() {
			key.createdAt = time.Now()
		}
		r.cache[host] = key
		return
	}
	if strings.TrimSpace(key.APIKey) != "" {
		existing.APIKey = key.APIKey
	}
	if strings.TrimSpace(key.VisitorData) != "" {
		existing.VisitorData = key.VisitorData
	}
	if strings.TrimSpace(key.DelegatedSessionID) != "" {
		existing.DelegatedSessionID = key.DelegatedSessionID
	}
	if strings.TrimSpace(key.UserSessionID) != "" {
		existing.UserSessionID = key.UserSessionID
	}
	if key.SessionIndex != nil {
		existing.SessionIndex = key.SessionIndex
	}
	if key.SignatureTimestamp != 0 {
		existing.SignatureTimestamp = key.SignatureTimestamp
	}
	r.cache[host] = existing
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

	body, err := iox.ReadAllLimit(resp.Body, 10<<20) // 10 MB watch page limit
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
	body, err := iox.ReadAllLimit(resp.Body, 20<<20) // 20 MB player JS limit
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
	case id == "web_creator":
		return "https://studio.youtube.com"
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
