package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/famomatic/ytv1/internal/challenge"
	"github.com/famomatic/ytv1/internal/formats"
	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/orchestrator"
	"github.com/famomatic/ytv1/internal/playerjs"
	"github.com/famomatic/ytv1/internal/policy"
	"github.com/famomatic/ytv1/internal/source"
	"github.com/famomatic/ytv1/internal/types"
)

// Client is the high-level YouTube client.
type Client struct {
	lastSessionSweep time.Time
	config           Config
	engine           *orchestrator.Engine
	playerJSResolver playerjs.Resolver
	logger           Logger
	// sources are non-YouTube extractors (e.g. SOOP) consulted before the
	// built-in YouTube path. The first whose Matches(input) returns true handles
	// the request.
	sources      []source.Source
	sessionsMu   sync.RWMutex
	sessions     map[string]videoSession
	challengesMu sync.RWMutex
	challenges   map[string]challengeSolutions
	// fetchLocks serializes per-video-id fetch paths (session build,
	// player URL resolution, challenge solve) to avoid thundering-herd
	// duplicate network requests and lost cache updates.
	fetchLocks     *keyLock
	fetchLocksOnce sync.Once
}

// fetchLock returns the per-video-id lock manager, initializing it lazily
// on first use so clients constructed directly (e.g. in tests) are still
// race-safe without forcing every caller through NewClient.
func (c *Client) fetchLock() *keyLock {
	c.fetchLocksOnce.Do(func() {
		if c.fetchLocks == nil {
			c.fetchLocks = newKeyLock()
		}
	})
	return c.fetchLocks
}

type videoSession struct {
	Response      *innertube.PlayerResponse
	PlayerURL     string
	Info          *VideoInfo
	CachedAt      time.Time
	ExpiresAt     time.Time
	expiryChecked bool
	valid         func() bool
	// MediaHeaders are extra HTTP headers a non-YouTube source requires on
	// manifest/segment requests (e.g. SOOP's Referer/Origin). Nil for YouTube.
	MediaHeaders http.Header
	// SourceName tags the extractor that produced this session ("" = YouTube).
	SourceName string
	// lastAccess tracks the most recent read/write time for LRU eviction.
	// Stored as a *atomic.Int64 (UnixNano) so reads under the RLock can
	// update access time without upgrading to a write lock, avoiding
	// serialization of concurrent GetVideo/ResolveStreamURL callers that
	// share the same session cache. A pointer is used because atomic types
	// cannot be stored directly as map values (alignment constraints).
	lastAccess *atomic.Int64
}

// defaultSessionCacheTTL is applied when SessionCacheTTL is unset (zero).
// YouTube googlevideo URLs typically expire 6 hours after issuance, so this
// matches the effective URL lifetime and prevents stale-URL reuse without
// forcing every caller to configure a TTL. An explicit zero still disables
// local TTL expiration (the opt-out escape hatch).
const defaultSessionCacheTTL = 6 * time.Hour

// urlExpirySafetyMargin is subtracted from a URL's expire timestamp before
// comparing against the current time, so a URL is treated as expired
// slightly before YouTube actually rejects it. This avoids races where a
// URL passes validation but expires during the in-flight request.
const urlExpirySafetyMargin = 30 * time.Second

// effectiveSessionCacheTTL returns the configured TTL, defaulting to
// defaultSessionCacheTTL when unset (zero). A negative value disables
// local TTL expiration (the opt-out escape hatch); URL-expire validation
// still applies independently.
func effectiveSessionCacheTTL(ttl time.Duration) time.Duration {
	if ttl == 0 {
		return defaultSessionCacheTTL
	}
	if ttl < 0 {
		return 0
	}
	return ttl
}

// sessionURLsExpired reports whether the cached session's format URLs have
// expired according to their own `expire` query parameter. YouTube embeds a
// UNIX timestamp (seconds) in each googlevideo URL; once it passes, the URL
// returns 403. This check is independent of SessionCacheTTL so server-side
// URL expiry is always honored. Returns false when no URL carries an
// expire param (e.g. ciphered-only or manifest sessions).
func sessionURLsExpired(resp *innertube.PlayerResponse, now time.Time) bool {
	expiry := sessionURLExpiry(videoSession{Response: resp})
	return !expiry.IsZero() && !now.Before(expiry)
}
func sessionURLExpiry(s videoSession) time.Time {
	var earliest time.Time
	check := func(raw string) {
		u, err := url.Parse(raw)
		if err != nil {
			return
		}
		for _, key := range []string{"expire", "expires"} {
			n, err := strconv.ParseInt(u.Query().Get(key), 10, 64)
			if err != nil || n <= 0 {
				continue
			}
			t := time.Unix(n, 0).Add(-urlExpirySafetyMargin)
			if earliest.IsZero() || t.Before(earliest) {
				earliest = t
			}
		}
	}
	if s.Response != nil {
		data := s.Response.StreamingData
		check(data.DashManifestURL)
		check(data.HlsManifestURL)
		for _, list := range [][]innertube.Format{data.Formats, data.AdaptiveFormats} {
			for _, f := range list {
				check(f.URL)
				for _, cipher := range []string{f.SignatureCipher, f.Cipher} {
					q, _ := url.ParseQuery(cipher)
					check(q.Get("url"))
				}
			}
		}
	}
	if s.Info != nil {
		check(s.Info.DashManifestURL)
		check(s.Info.HLSManifestURL)
		for _, f := range s.Info.Formats {
			check(f.URL)
			check(f.ManifestURL)
			for _, part := range f.Parts {
				check(part)
			}
		}
	}
	return earliest
}
func sessionExpired(s videoSession, now time.Time) bool {
	if s.valid != nil && !s.valid() {
		return true
	}
	expiry := s.ExpiresAt
	if !s.expiryChecked {
		expiry = sessionURLExpiry(s)
	}
	return !expiry.IsZero() && !now.Before(expiry)
}

// New creates a new YouTube client.
func New(config Config) *Client {
	return NewClient(config)
}

// NewClient creates a new YouTube client.
func NewClient(config Config) *Client {
	if config.HTTPClient == nil {
		config.HTTPClient = defaultHTTPClient(config.ProxyURL, config.SourceAddress, config.InsecureSkipVerify)
	} else {
		// Avoid mutating a caller-owned *http.Client (which may be shared
		// across many ytv1 clients or used elsewhere) when we only need to
		// attach a cookie jar. Take a shallow copy so the jar assignment is
		// local to this client.
		owned := *config.HTTPClient
		config.HTTPClient = &owned
	}
	if config.CookieJar != nil {
		config.HTTPClient.Jar = config.CookieJar
	}
	if config.PoTokenProvider != nil {
		config.PoTokenProvider = challenge.NewCachedPoTokenProviderWithTTL(config.PoTokenProvider, config.PoTokenCacheTTL)
	}

	registry := innertube.NewRegistry()
	innerCfg := config.ToInnerTubeConfig()
	preferAuthDefaults := config.CookieJar != nil || (config.HTTPClient != nil && config.HTTPClient.Jar != nil)
	selector := policy.NewSelector(registry, innerCfg.ClientOverrides, innerCfg.ClientSkip, preferAuthDefaults)
	engine := orchestrator.NewEngine(selector, innerCfg)
	playerHeaders := cloneHeader(innerCfg.RequestHeaders)
	if playerHeaders == nil {
		playerHeaders = make(http.Header)
	}
	mergeHeaders(playerHeaders, innerCfg.PlayerJSHeaders)
	jsResolver := playerjs.NewResolver(
		config.HTTPClient,
		playerjs.NewMemoryCache(),
		playerjs.ResolverConfig{
			BaseURL:         innerCfg.PlayerJSBaseURL,
			UserAgent:       innerCfg.PlayerJSUserAgent,
			Headers:         playerHeaders,
			PreferredLocale: innerCfg.PlayerJSPreferredLocale,
		},
	)
	logger := config.Logger
	if logger == nil {
		logger = nopLogger{}
	}

	return &Client{
		config:           config,
		engine:           engine,
		playerJSResolver: jsResolver,
		logger:           logger,
		sources:          source.Build(source.Deps{HTTPClient: config.HTTPClient}),
		sessions:         make(map[string]videoSession),
		challenges:       make(map[string]challengeSolutions),
		fetchLocks:       newKeyLock(),
	}
}

// HTTPClient returns the configured HTTP client used for network requests.
func (c *Client) HTTPClient() *http.Client {
	if c == nil || c.config.HTTPClient == nil {
		return nil
	}
	return c.config.HTTPClient
}

// GetVideo fetches video metadata and normalized formats for the input ID/URL.
func (c *Client) GetVideo(ctx context.Context, input string) (*VideoInfo, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	// Non-YouTube sources (e.g. SOOP) are dispatched before the YouTube path.
	if src := source.Match(c.sources, input); src != nil {
		return c.getVideoViaSource(ctx, src, input)
	}

	videoID, err := normalizeVideoID(input)
	if err != nil {
		return nil, err
	}

	if cached, ok := c.getSession(videoID); ok && cached.Info != nil {
		return cloneVideoInfo(cached.Info), nil
	}
	// Serialize per-video-id extraction so concurrent callers share one
	// fetch and cache write instead of racing duplicate network calls.
	lock, release := c.fetchLock().acquire(videoID)
	defer release()
	if err := lock.LockContext(ctx); err != nil {
		return nil, err
	}
	defer lock.Unlock()

	if cached, ok := c.getSession(videoID); ok && cached.Info != nil {
		return cloneVideoInfo(cached.Info), nil
	}
	return c.fetchVideoOnce(ctx, videoID)
}

// fetchVideoOnce performs the actual extraction and session caching. It is
// called while holding the per-video-id fetch lock.
func (c *Client) fetchVideoOnce(ctx context.Context, videoID string) (*VideoInfo, error) {
	resp, err := c.engine.GetVideoInfo(ctx, videoID)
	if err != nil {
		return nil, mapError(err)
	}

	parsedFormats := formats.Parse(resp)

	outFormats := make([]FormatInfo, 0, len(parsedFormats))
	for _, f := range parsedFormats {
		outFormats = append(outFormats, toFormatInfo(f))
	}
	thumbnail := bestThumbnail(resp)

	info := &VideoInfo{
		ID:              resp.VideoDetails.VideoID,
		Title:           resp.VideoDetails.Title,
		Author:          resp.VideoDetails.Author,
		Description:     firstNonEmptyString(resp.VideoDetails.ShortDescription, resp.Microformat.PlayerMicroformatRenderer.Description.SimpleText),
		DurationSec:     parseInt64String(firstNonEmptyString(resp.VideoDetails.LengthSeconds, resp.Microformat.PlayerMicroformatRenderer.LengthSeconds)),
		ViewCount:       parseInt64String(firstNonEmptyString(resp.VideoDetails.ViewCount, resp.Microformat.PlayerMicroformatRenderer.ViewCount)),
		ChannelID:       firstNonEmptyString(resp.VideoDetails.ChannelID, resp.Microformat.PlayerMicroformatRenderer.ExternalChannelId),
		PublishDate:     resp.Microformat.PlayerMicroformatRenderer.PublishDate,
		UploadDate:      resp.Microformat.PlayerMicroformatRenderer.UploadDate,
		Category:        resp.Microformat.PlayerMicroformatRenderer.Category,
		IsLive:          resp.VideoDetails.IsLiveContent || resp.PlayabilityStatus.IsLive(),
		Keywords:        append([]string(nil), resp.VideoDetails.Keywords...),
		ThumbnailURL:    thumbnail.URL,
		ThumbnailWidth:  thumbnail.Width,
		ThumbnailHeight: thumbnail.Height,
		Formats:         outFormats,
		DashManifestURL: resp.StreamingData.DashManifestURL,
		HLSManifestURL:  resp.StreamingData.HlsManifestURL,
	}

	playerURL := ""
	nChallenges, sigChallenges := collectStreamChallenges(resp, info.DashManifestURL, info.HLSManifestURL)
	if len(nChallenges) > 0 || len(sigChallenges) > 0 {
		fetched, fetchErr := c.fetchPlayerURL(ctx, videoID)
		if fetchErr == nil {
			playerURL = fetched
			c.primeChallengeSolutions(ctx, playerURL, resp, info.DashManifestURL, info.HLSManifestURL)
		}
	}
	info.DashManifestURL = c.resolveManifestURL(ctx, info.DashManifestURL, playerURL, resp.SourceClient, innertube.StreamingProtocolDASH)
	info.HLSManifestURL = c.resolveManifestURL(ctx, info.HLSManifestURL, playerURL, resp.SourceClient, innertube.StreamingProtocolHLS)

	manifestFormats := c.loadManifestFormats(ctx, info.DashManifestURL, info.HLSManifestURL)
	for i := range manifestFormats {
		manifestFormats[i].SourceClient = resp.SourceClient
		manifestFormats[i].ThisIsLive = info.IsLive
	}
	if len(manifestFormats) > 0 {
		info.Formats = appendUniqueFormats(info.Formats, manifestFormats)
	}
	c.putSession(videoID, videoSession{
		Response:  resp,
		PlayerURL: playerURL,
		Info:      cloneVideoInfo(info),
	})

	return info, nil
}

// matchSource returns the non-YouTube source that recognizes input, or nil.
func (c *Client) matchSource(input string) source.Source {
	return source.Match(c.sources, input)
}

// getVideoViaSource extracts metadata/formats through a non-YouTube source and
// caches the result as a session keyed by "<source>:<id>", so a subsequent
// download reuses it and can attach the source's required media headers.
func (c *Client) getVideoViaSource(ctx context.Context, src source.Source, input string) (*VideoInfo, error) {
	key := sourceInputKey(src.Name(), input)
	if cached, ok := c.getSession(key); ok && cached.Info != nil {
		return cloneVideoInfo(cached.Info), nil
	}
	lock, release := c.fetchLock().acquire(key)
	defer release()
	if err := lock.LockContext(ctx); err != nil {
		return nil, err
	}
	defer lock.Unlock()
	if cached, ok := c.getSession(key); ok && cached.Info != nil {
		return cloneVideoInfo(cached.Info), nil
	}
	media, err := src.Extract(ctx, input)
	if err != nil {
		return nil, err
	}
	if media == nil || len(media.Formats) == 0 {
		return nil, ErrNoPlayableFormats
	}
	info := mediaToVideoInfo(media)
	info.SourceName = src.Name()
	info.WebpageURL = firstNonEmptyString(media.WebpageURL, input)
	c.cacheSourceSession(src.Name(), input, info, media.MediaHeaders)
	return info, nil
}

// cacheSourceSession stores an extracted source result under two keys: the
// media-id key (used by the download pipeline to resolve URLs and attach the
// source's media headers) and the input key (so a following download reuses the
// extraction instead of re-running it — see downloadViaSource).
func (c *Client) cacheSourceSession(name, input string, info *VideoInfo, headers http.Header) {
	sess := videoSession{
		Info:         cloneVideoInfo(info),
		MediaHeaders: cloneHeader(headers),
		SourceName:   name,
	}
	for _, src := range c.sources {
		if src.Name() != name {
			continue
		}
		if validator, ok := src.(interface{ MediaURLValid(string) bool }); ok {
			urls := make([]string, len(info.Formats))
			for i, f := range info.Formats {
				urls[i] = f.URL
			}
			sess.valid = func() bool {
				for _, u := range urls {
					if !validator.MediaURLValid(u) {
						return false
					}
				}
				return true
			}
		}
	}
	c.putSession(sourceSessionKey(name, info.ID), sess)
	c.putSession(sourceInputKey(name, input), sess)
}

// sourceSessionKey namespaces a source-scoped id so it cannot collide with an
// 11-char YouTube video id.
func sourceSessionKey(name, id string) string {
	return name + ":" + id
}

// sourceInputKey namespaces a source-scoped input URL. The NUL byte cannot
// appear in a URL or in a sourceSessionKey, so the two key spaces never collide.
func sourceInputKey(name, input string) string {
	return name + "\x00" + input
}

// mediaToVideoInfo maps a site-neutral source.Media to the public VideoInfo.
func mediaToVideoInfo(m *source.Media) *VideoInfo {
	return cloneVideoInfo(&VideoInfo{
		ID:           m.ID,
		Title:        m.Title,
		Author:       m.Author,
		Description:  m.Description,
		DurationSec:  m.DurationSec,
		IsLive:       m.IsLive,
		UploadDate:   m.UploadDate,
		ThumbnailURL: m.ThumbnailURL,
		Formats:      append([]FormatInfo(nil), m.Formats...),
	})
}

// sessionMediaHeaders returns any extra media request headers cached for the
// given session id (nil for YouTube or unknown ids).
func (c *Client) sessionMediaHeaders(videoID string) http.Header {
	if s, ok := c.getSession(videoID); ok {
		return s.MediaHeaders
	}
	return nil
}

// GetFormats returns normalized formats only.
func (c *Client) GetFormats(ctx context.Context, input string) ([]FormatInfo, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	v, err := c.GetVideo(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(v.Formats) == 0 {
		return nil, ErrNoPlayableFormats
	}
	return v.Formats, nil
}

// FetchDASHManifest fetches DASH manifest content for the given video ID/URL.
func (c *Client) FetchDASHManifest(ctx context.Context, input string) (string, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	session, videoID, err := c.ensureSession(ctx, input)
	if err != nil {
		return "", err
	}
	manifestURL := c.resolveManifestURL(
		ctx,
		session.Response.StreamingData.DashManifestURL,
		session.PlayerURL,
		session.Response.SourceClient,
		innertube.StreamingProtocolDASH,
	)
	if manifestURL == "" {
		return "", fmt.Errorf("%w: dash manifest unavailable for video=%s", ErrNoPlayableFormats, videoID)
	}
	manifest, err := formats.FetchDASHManifest(ctx, c.config.HTTPClient, manifestURL, c.config.RequestHeaders)
	if err != nil {
		return "", err
	}
	return manifest.RawContent, nil
}

// FetchHLSManifest fetches HLS manifest content for the given video ID/URL.
func (c *Client) FetchHLSManifest(ctx context.Context, input string) (string, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	session, videoID, err := c.ensureSession(ctx, input)
	if err != nil {
		return "", err
	}
	manifestURL := c.resolveManifestURL(
		ctx,
		session.Response.StreamingData.HlsManifestURL,
		session.PlayerURL,
		session.Response.SourceClient,
		innertube.StreamingProtocolHLS,
	)
	if manifestURL == "" {
		return "", fmt.Errorf("%w: hls manifest unavailable for video=%s", ErrNoPlayableFormats, videoID)
	}
	manifest, err := formats.FetchHLSManifest(ctx, c.config.HTTPClient, manifestURL, c.config.RequestHeaders)
	if err != nil {
		return "", err
	}
	return manifest.RawContent, nil
}

// findSourceSession returns a cached non-YouTube source session whose media id
// is videoID, if any, by probing each registered source's namespaced key.
func (c *Client) findSourceSession(videoID string) (videoSession, bool) {
	for _, src := range c.sources {
		if src == nil {
			continue
		}
		if s, ok := c.getSession(sourceSessionKey(src.Name(), videoID)); ok && s.SourceName != "" {
			return s, true
		}
	}
	return videoSession{}, false
}

// ResolveStreamURL resolves a direct playable URL for a specific itag.
func (c *Client) ResolveStreamURL(ctx context.Context, videoID string, itag int) (string, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	// Non-YouTube source formats already carry a direct URL (resolved during
	// extraction); return it without the YouTube id-normalization / player
	// pipeline, which would reject a source id. This is what backs `-g` /
	// `--print url` for SOOP.
	if s, ok := c.findSourceSession(videoID); ok {
		for _, f := range s.Info.Formats {
			if f.Itag == itag {
				if strings.TrimSpace(f.URL) == "" {
					return "", fmt.Errorf("%w: itag=%d", ErrNoPlayableFormats, itag)
				}
				return f.URL, nil
			}
		}
		return "", fmt.Errorf("%w: itag=%d", ErrNoPlayableFormats, itag)
	}

	videoID, err := normalizeVideoID(videoID)
	if err != nil {
		return "", err
	}

	lock, release := c.fetchLock().acquire(videoID)
	defer release()
	if err := lock.LockContext(ctx); err != nil {
		return "", err
	}
	defer lock.Unlock()

	return c.resolveStreamURLLocked(ctx, videoID, itag)
}

// resolveStreamURLLocked resolves a direct playable URL for a specific itag.
// The caller must already hold the per-video-id fetch lock.
func (c *Client) resolveStreamURLLocked(ctx context.Context, videoID string, itag int) (string, error) {
	session, ok := c.getSession(videoID)
	if !ok {
		if _, err := c.fetchVideoOnce(ctx, videoID); err != nil {
			return "", err
		}
		session, ok = c.getSession(videoID)
		if !ok {
			return "", ErrChallengeNotSolved
		}
	}

	raw, found := findRawFormat(session.Response, itag)
	if !found && session.Info != nil {
		for _, f := range session.Info.Formats {
			if f.Itag == itag && f.URL != "" {
				if f.ManifestURL != "" && session.Info != nil && f.URL == session.Info.DashManifestURL {
					return f.URL, nil
				}
				return c.resolveDirectURL(ctx, f.URL, session.PlayerURL, f.SourceClient, protocolFromFormat(f))
			}
		}
	}
	if !found {
		return "", fmt.Errorf("%w: itag=%d", ErrNoPlayableFormats, itag)
	}

	if raw.URL != "" {
		if hasQueryParam(raw.URL, "n") && strings.TrimSpace(session.PlayerURL) == "" {
			updated, fetchErr := c.ensureSessionPlayerURL(ctx, videoID, session)
			if fetchErr != nil {
				return "", ErrChallengeNotSolved
			}
			session = updated
		}
		rewritten, err := c.resolveDirectURL(
			ctx,
			raw.URL,
			session.PlayerURL,
			firstNonEmptyString(raw.SourceClient, session.Response.SourceClient),
			protocolFromRawFormat(raw),
		)
		if err != nil {
			return "", err
		}
		return rewritten, nil
	}

	cipher := raw.SignatureCipher
	if cipher == "" {
		cipher = raw.Cipher
	}
	if cipher == "" {
		return "", ErrChallengeNotSolved
	}
	if strings.TrimSpace(session.PlayerURL) == "" {
		updated, fetchErr := c.ensureSessionPlayerURL(ctx, videoID, session)
		if fetchErr != nil {
			return "", ErrChallengeNotSolved
		}
		session = updated
	}

	params, err := url.ParseQuery(cipher)
	if err != nil {
		return "", ErrChallengeNotSolved
	}
	rawURL := params.Get("url")
	if rawURL == "" {
		return "", ErrChallengeNotSolved
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", ErrChallengeNotSolved
	}

	if s := params.Get("s"); s != "" {
		decSig, err := c.decodeSignatureWithCache(ctx, session.PlayerURL, s)
		if err != nil {
			return "", ErrChallengeNotSolved
		}
		sp := params.Get("sp")
		if sp == "" {
			sp = "signature"
		}
		q := u.Query()
		q.Set(sp, decSig)
		u.RawQuery = q.Encode()
	}

	q := u.Query()
	if n := q.Get("n"); n != "" {
		decN, err := c.decodeNWithCache(ctx, session.PlayerURL, n)
		if err != nil {
			c.warnf("n challenge decode failed for video=%s itag=%d; using original n value: %v", videoID, itag, err)
		} else {
			q.Set("n", decN)
			u.RawQuery = q.Encode()
		}
	}

	rewritten, err := c.applyPoTokenPolicyToURL(ctx, u.String(), firstNonEmptyString(raw.SourceClient, session.Response.SourceClient), protocolFromRawFormat(raw))
	if err != nil {
		return "", err
	}
	return rewritten, nil
}

func (c *Client) resolveSelectedFormatURL(ctx context.Context, videoID string, f FormatInfo) (string, error) {
	// Source-backed sessions (e.g. SOOP) carry final, already-resolved media
	// URLs — the YouTube signature/n-sig/PO-token rewriting below does not apply
	// and normalizeVideoID would reject the "<source>:<id>" key.
	if s, ok := c.getSession(videoID); ok && s.SourceName != "" {
		return f.URL, nil
	}

	videoID, err := normalizeVideoID(videoID)
	if err != nil {
		return "", err
	}

	lock, release := c.fetchLock().acquire(videoID)
	defer release()
	if err := lock.LockContext(ctx); err != nil {
		return "", err
	}
	defer lock.Unlock()

	if strings.TrimSpace(f.URL) != "" {
		session, ok := c.getSession(videoID)
		refreshed := !ok
		if !ok {
			if _, err := c.fetchVideoOnce(ctx, videoID); err != nil {
				return "", err
			}
			session, ok = c.getSession(videoID)
			if !ok {
				return "", ErrChallengeNotSolved
			}
		}
		if refreshed && session.Info != nil {
			found := false
			for _, fresh := range session.Info.Formats {
				if fresh.Itag == f.Itag {
					f = fresh
					found = true
					break
				}
			}
			if !found {
				return "", ErrNoPlayableFormats
			}
			if f.Ciphered || f.URL == "" {
				return c.resolveStreamURLLocked(ctx, videoID, f.Itag)
			}
		}
		if hasQueryParam(f.URL, "n") && strings.TrimSpace(session.PlayerURL) == "" {
			updated, fetchErr := c.ensureSessionPlayerURL(ctx, videoID, session)
			if fetchErr != nil {
				return "", ErrChallengeNotSolved
			}
			session = updated
		}
		if f.ManifestURL != "" && session.Info != nil && f.URL == session.Info.DashManifestURL {
			return f.URL, nil
		}
		return c.resolveDirectURL(ctx, f.URL, session.PlayerURL, f.SourceClient, protocolFromFormat(f))
	}

	// resolveSelectedFormatURL already holds the per-video-id fetch lock;
	// call the lock-free inner path to avoid a self-reentrant deadlock.
	return c.resolveStreamURLLocked(ctx, videoID, f.Itag)
}

func toFormatInfo(f formats.Format) FormatInfo {
	hasVideo := f.HasVideo
	hasAudio := f.HasAudio
	return FormatInfo{
		Itag:              f.Itag,
		ManifestURL:       f.ManifestURL,
		RepresentationID:  f.RepresentationID,
		URL:               f.URL,
		MimeType:          f.MimeType,
		Protocol:          f.Protocol,
		HasAudio:          hasAudio,
		HasVideo:          hasVideo,
		Bitrate:           f.Bitrate,
		Width:             f.Width,
		Height:            f.Height,
		FPS:               f.FPS,
		Ciphered:          f.Ciphered,
		IsDRM:             f.IsDRM,
		IsDamaged:         f.IsDamaged,
		Quality:           f.Quality,
		QualityLabel:      f.QualityLabel,
		SourceClient:      f.SourceClient,
		ContentLength:     f.ContentLength,
		TargetDurationSec: f.TargetDurationSec,
		Incomplete:        f.Incomplete,
		ThisIsLive:        f.ThisIsLive,
	}
}

func normalizeVideoID(input string) (string, error) {
	id, err := ExtractVideoID(input)
	if err == nil {
		return id, nil
	}
	if errors.Is(err, ErrInvalidInput) {
		return "", err
	}
	return "", ErrInvalidInput
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, types.ErrNoClientsAvailable):
		return &AllClientsFailedDetailError{}
	case errors.Is(err, types.ErrLoginRequired):
		return ErrLoginRequired
	case errors.Is(err, types.ErrVideoUnavailable):
		return ErrUnavailable
	case errors.Is(err, types.ErrAgeRestricted):
		return ErrUnavailable
	}

	var playabilityErr *orchestrator.PlayabilityError
	if errors.As(err, &playabilityErr) {
		attempts := []AttemptDetail{attemptDetailFromSingle(playabilityErr.Client, playabilityErr)}
		if playabilityErr.RequiresLogin() || playabilityErr.IsAgeRestricted() {
			return &LoginRequiredDetailError{Attempts: attempts}
		}
		return &UnavailableDetailError{Attempts: attempts}
	}

	var allFailedErr *orchestrator.AllClientsFailedError
	if errors.As(err, &allFailedErr) {
		attempts := make([]AttemptDetail, 0, len(allFailedErr.Attempts))
		hasUnavailable := false
		hasLoginRequired := false
		for _, attempt := range allFailedErr.Attempts {
			attempts = append(attempts, attemptDetailFromSingle(attempt.Client, attempt.Err))
			if !errors.As(attempt.Err, &playabilityErr) {
				continue
			}
			if playabilityErr.RequiresLogin() || playabilityErr.IsAgeRestricted() {
				hasLoginRequired = true
			}
			if playabilityErr.IsGeoRestricted() || playabilityErr.IsUnavailable() {
				hasUnavailable = true
			}
		}
		if hasLoginRequired {
			return &LoginRequiredDetailError{Attempts: attempts}
		}
		if hasUnavailable {
			return &UnavailableDetailError{Attempts: attempts}
		}
		return &AllClientsFailedDetailError{Attempts: attempts}
	}

	var httpStatusErr *orchestrator.HTTPStatusError
	if errors.As(err, &httpStatusErr) {
		return &AllClientsFailedDetailError{
			Attempts: []AttemptDetail{attemptDetailFromSingle(httpStatusErr.Client, httpStatusErr)},
		}
	}
	var poTokenErr *orchestrator.PoTokenRequiredError
	if errors.As(err, &poTokenErr) {
		return &AllClientsFailedDetailError{
			Attempts: []AttemptDetail{attemptDetailFromSingle(poTokenErr.Client, poTokenErr)},
		}
	}

	return err
}

func attemptDetailFromSingle(client string, err error) AttemptDetail {
	d := AttemptDetail{
		Client: client,
		Stage:  "unknown",
	}
	if err == nil {
		return d
	}

	d.Reason = err.Error()

	var playabilityErr *orchestrator.PlayabilityError
	if errors.As(err, &playabilityErr) {
		d.Stage = "playability"
		d.Reason = playabilityErr.Status + ": " + playabilityErr.Reason
		d.PlayabilityStatus = playabilityErr.Status
		d.PlayabilityReason = playabilityErr.Reason
		d.PlayabilitySubreason = playabilityErr.Detail.Subreason
		d.GeoRestricted = playabilityErr.IsGeoRestricted()
		d.LoginRequired = playabilityErr.RequiresLogin()
		d.AgeRestricted = playabilityErr.IsAgeRestricted()
		d.Unavailable = playabilityErr.IsUnavailable()
		d.DRMProtected = playabilityErr.IsDRMProtected()
		d.AvailableCountries = append([]string(nil), playabilityErr.Detail.AvailableCountries...)
		return d
	}

	var httpStatusErr *orchestrator.HTTPStatusError
	if errors.As(err, &httpStatusErr) {
		d.Stage = "request"
		d.HTTPStatus = httpStatusErr.StatusCode
		return d
	}

	var poTokenErr *orchestrator.PoTokenRequiredError
	if errors.As(err, &poTokenErr) {
		d.Stage = "pot"
		d.POTRequired = true
		d.POTAvailable = poTokenErr.ProviderAvailable
		d.POTPolicy = string(poTokenErr.Policy)
		if len(poTokenErr.Protocols) > 0 {
			d.POTProtocols = make([]string, 0, len(poTokenErr.Protocols))
			for _, protocol := range poTokenErr.Protocols {
				d.POTProtocols = append(d.POTProtocols, string(protocol))
			}
		}
		d.Reason = poTokenErr.Cause
		return d
	}

	return d
}

func (c *Client) getSession(videoID string) (videoSession, bool) {
	// Fast path: an RLock lets concurrent readers share the session cache
	// without serializing against each other. lastAccess is updated
	// atomically for LRU eviction accuracy without upgrading to a write
	// lock.
	c.sessionsMu.RLock()
	s, ok := c.sessions[videoID]
	if ok {
		expired := false
		if ttl := effectiveSessionCacheTTL(c.config.SessionCacheTTL); ttl > 0 && !s.CachedAt.IsZero() && time.Since(s.CachedAt) > ttl {
			expired = true
		}
		// URL-expire validation runs regardless of TTL so server-side URL
		// expiry is always honored even when the local TTL is disabled.
		if !expired && sessionExpired(s, time.Now()) {
			expired = true
		}
		if !expired {
			if s.lastAccess != nil {
				s.lastAccess.Store(time.Now().UnixNano())
			}
			c.sessionsMu.RUnlock()
			return s, true
		}
	}
	c.sessionsMu.RUnlock()

	if !ok {
		return videoSession{}, false
	}

	// Slow path: the entry exists but is expired. Upgrade to a write lock
	// (after releasing the RLock to avoid self-deadlock) and remove it so
	// the map does not accumulate stale entries between putSession calls.
	// Re-check under the write lock because a concurrent putSession may have
	// already refreshed or removed it.
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	cur, ok := c.sessions[videoID]
	if !ok {
		return videoSession{}, false
	}
	ttlExpired := effectiveSessionCacheTTL(c.config.SessionCacheTTL) > 0 && !cur.CachedAt.IsZero() && time.Since(cur.CachedAt) > effectiveSessionCacheTTL(c.config.SessionCacheTTL)
	if ttlExpired || sessionExpired(cur, time.Now()) {
		delete(c.sessions, videoID)
		return videoSession{}, false
	}
	if cur.lastAccess != nil {
		cur.lastAccess.Store(time.Now().UnixNano())
	}
	return cur, true
}

func (c *Client) putSession(videoID string, session videoSession) {
	now := time.Now()
	session.ExpiresAt = sessionURLExpiry(session)
	session.expiryChecked = true
	if session.CachedAt.IsZero() {
		session.CachedAt = now
	}
	if session.lastAccess == nil {
		session.lastAccess = new(atomic.Int64)
	}
	session.lastAccess.Store(now.UnixNano())

	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	if c.sessions == nil {
		c.sessions = make(map[string]videoSession)
	}
	if now.Sub(c.lastSessionSweep) >= time.Minute {
		c.evictExpiredLocked(now)
		c.lastSessionSweep = now
	}
	c.sessions[videoID] = session
	c.evictLRULocked()
}
func (c *Client) deleteSession(videoID string) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	delete(c.sessions, videoID)
}

func (c *Client) evictExpiredLocked(now time.Time) {
	ttl := effectiveSessionCacheTTL(c.config.SessionCacheTTL)
	// A zero TTL is the documented opt-out: skip TTL-based eviction but
	// still drop sessions whose URLs have expired server-side.
	for id, session := range c.sessions {
		if ttl > 0 && !session.CachedAt.IsZero() && now.Sub(session.CachedAt) > ttl {
			delete(c.sessions, id)
			continue
		}
		if sessionExpired(session, now) {
			delete(c.sessions, id)
		}
	}
}

func (c *Client) evictLRULocked() {
	maxEntries := c.config.SessionCacheMaxEntries
	if maxEntries == 0 {
		maxEntries = 256
	}
	if maxEntries < 0 {
		return
	}
	for len(c.sessions) > maxEntries {
		var oldestID string
		var oldest int64
		first := true
		for id, session := range c.sessions {
			candidate := int64(0)
			if session.lastAccess != nil {
				candidate = session.lastAccess.Load()
			}
			if candidate == 0 {
				// Fall back to CachedAt for sessions populated before any read.
				candidate = session.CachedAt.UnixNano()
			}
			if first || candidate < oldest {
				first = false
				oldestID = id
				oldest = candidate
			}
		}
		if oldestID == "" {
			return
		}
		delete(c.sessions, oldestID)
	}
}

func (c *Client) ensureSession(ctx context.Context, input string) (videoSession, string, error) {
	videoID, err := normalizeVideoID(input)
	if err != nil {
		return videoSession{}, "", err
	}

	// Fast path: a cached session may exist without taking the fetch lock.
	if session, ok := c.getSession(videoID); ok {
		return session, videoID, nil
	}

	lock, release := c.fetchLock().acquire(videoID)
	defer release()
	if err := lock.LockContext(ctx); err != nil {
		return videoSession{}, "", err
	}
	defer lock.Unlock()

	// Re-check once inside the lock: another caller may have populated it.
	if session, ok := c.getSession(videoID); ok {
		return session, videoID, nil
	}

	if _, err := c.fetchVideoOnce(ctx, videoID); err != nil {
		return videoSession{}, "", err
	}
	session, ok := c.getSession(videoID)
	if !ok {
		return videoSession{}, "", ErrChallengeNotSolved
	}
	return session, videoID, nil
}

func findRawFormat(resp *innertube.PlayerResponse, itag int) (innertube.Format, bool) {
	if resp == nil {
		return innertube.Format{}, false
	}
	for _, f := range resp.StreamingData.Formats {
		if f.Itag == itag {
			return f, true
		}
	}
	for _, f := range resp.StreamingData.AdaptiveFormats {
		if f.Itag == itag {
			return f, true
		}
	}
	return innertube.Format{}, false
}

func (c *Client) fetchPlayerURL(ctx context.Context, videoID string) (string, error) {
	c.emitExtractionEvent("webpage", "start", "web", videoID)
	playerURL, err := c.playerJSResolver.GetPlayerURL(ctx, videoID)
	if err != nil {
		c.emitExtractionEvent("webpage", "failure", "web", err.Error())
		return "", err
	}
	c.emitExtractionEvent("webpage", "success", "web", playerURL)
	return playerURL, nil
}

func (c *Client) ensureSessionPlayerURL(ctx context.Context, videoID string, session videoSession) (videoSession, error) {
	if strings.TrimSpace(session.PlayerURL) != "" {
		return session, nil
	}
	playerURL, err := c.fetchPlayerURL(ctx, videoID)
	if err != nil {
		return session, err
	}
	session.PlayerURL = playerURL
	c.putSession(videoID, session)
	return session, nil
}

func protocolFromRawFormat(raw innertube.Format) innertube.VideoStreamingProtocol {
	if p := protocolFromURL(raw.URL); p != innertube.StreamingProtocolUnknown {
		return p
	}
	cipher := raw.SignatureCipher
	if cipher == "" {
		cipher = raw.Cipher
	}
	if strings.TrimSpace(cipher) == "" {
		return innertube.StreamingProtocolUnknown
	}
	params, err := url.ParseQuery(cipher)
	if err != nil {
		return innertube.StreamingProtocolUnknown
	}
	return protocolFromURL(params.Get("url"))
}

func (c *Client) resolveManifestURL(
	ctx context.Context,
	manifestURL string,
	playerURL string,
	sourceClient string,
	protocol innertube.VideoStreamingProtocol,
) string {
	if manifestURL == "" {
		return ""
	}

	rewritten := manifestURL
	if playerURL != "" && hasQueryParam(manifestURL, "n") {
		nRewritten, err := rewriteURLParam(manifestURL, "n", func(value string) (string, error) {
			return c.decodeNWithCache(ctx, playerURL, value)
		})
		if err != nil {
			c.warnf("n challenge decode failed for manifest url; using original url: %v", err)
		} else {
			rewritten = nRewritten
		}
	}

	potRewritten, err := c.applyPoTokenPolicyToURL(ctx, rewritten, sourceClient, protocol)
	if err != nil {
		c.warnf("po token injection failed for manifest url; using original url: %v", err)
		return rewritten
	}
	return potRewritten
}

func hasQueryParam(rawURL, key string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Query().Get(key) != ""
}

func rewriteURLParam(rawURL, key string, decoder func(string) (string, error)) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	current := q.Get(key)
	if current == "" {
		return rawURL, nil
	}
	next, err := decoder(current)
	if err != nil {
		return "", err
	}
	q.Set(key, next)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *Client) loadManifestFormats(ctx context.Context, dashURL, hlsURL string) []FormatInfo {
	out := make([]FormatInfo, 0, 16)
	if dashURL != "" {
		c.emitExtractionEvent("manifest", "start", "dash", dashURL)
		if dash, err := formats.FetchDASHManifest(ctx, c.httpClient(), dashURL, c.config.RequestHeaders); err == nil {
			c.emitExtractionEvent("manifest", "success", "dash", dashURL)
			for _, f := range dash.Formats {
				out = append(out, toFormatInfo(f))
			}
		} else {
			c.emitExtractionEvent("manifest", "failure", "dash", err.Error())
		}
	}
	if hlsURL != "" {
		c.emitExtractionEvent("manifest", "start", "hls", hlsURL)
		if hls, err := formats.FetchHLSManifest(ctx, c.httpClient(), hlsURL, c.config.RequestHeaders); err == nil {
			c.emitExtractionEvent("manifest", "success", "hls", hlsURL)
			for _, f := range hls.Formats {
				out = append(out, toFormatInfo(f))
			}
		} else {
			c.emitExtractionEvent("manifest", "failure", "hls", err.Error())
		}
	}
	return out
}

func appendUniqueFormats(base []FormatInfo, extras []FormatInfo) []FormatInfo {
	if len(extras) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extras))
	keyOf := func(f FormatInfo) string {
		return fmt.Sprintf("%d|%s|%s", f.Itag, f.Protocol, f.URL)
	}
	out := make([]FormatInfo, 0, len(base)+len(extras))
	for _, f := range base {
		k := keyOf(f)
		if _, exists := seen[k]; exists {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, f)
	}
	for _, f := range extras {
		k := keyOf(f)
		if _, exists := seen[k]; exists {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, f)
	}
	return out
}

func (c *Client) resolveDirectURL(
	ctx context.Context,
	rawURL string,
	playerURL string,
	sourceClient string,
	protocol innertube.VideoStreamingProtocol,
) (string, error) {
	if rawURL == "" {
		return "", ErrChallengeNotSolved
	}

	rewritten := rawURL
	if hasQueryParam(rawURL, "n") {
		if playerURL == "" {
			return "", ErrChallengeNotSolved
		}
		nRewritten, err := rewriteURLParam(rawURL, "n", func(value string) (string, error) {
			return c.decodeNWithCache(ctx, playerURL, value)
		})
		if err != nil {
			c.warnf("n challenge decode failed for direct url; using original url: %v", err)
		} else {
			rewritten = nRewritten
		}
	}

	potRewritten, err := c.applyPoTokenPolicyToURL(ctx, rewritten, sourceClient, protocol)
	if err != nil {
		return "", err
	}
	return potRewritten, nil
}

func (c *Client) warnf(format string, args ...any) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.Warnf(format, args...)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseInt64String(raw string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func bestThumbnail(resp *innertube.PlayerResponse) innertube.Thumbnail {
	if resp == nil {
		return innertube.Thumbnail{}
	}
	candidates := make([]innertube.Thumbnail, 0,
		len(resp.VideoDetails.Thumbnail.Thumbnails)+len(resp.Microformat.PlayerMicroformatRenderer.Thumbnail.Thumbnails))
	candidates = append(candidates, resp.VideoDetails.Thumbnail.Thumbnails...)
	candidates = append(candidates, resp.Microformat.PlayerMicroformatRenderer.Thumbnail.Thumbnails...)
	var best innertube.Thumbnail
	for _, thumb := range candidates {
		if strings.TrimSpace(thumb.URL) == "" {
			continue
		}
		if best.URL == "" || thumbnailScore(thumb) > thumbnailScore(best) {
			best = thumb
		}
	}
	return best
}

func thumbnailScore(thumb innertube.Thumbnail) int {
	if thumb.Width > 0 && thumb.Height > 0 {
		return thumb.Width * thumb.Height
	}
	return thumb.Width + thumb.Height
}

func cloneVideoInfo(v *VideoInfo) *VideoInfo {
	if v == nil {
		return nil
	}
	clone := *v
	if len(v.Keywords) > 0 {
		clone.Keywords = append([]string(nil), v.Keywords...)
	}
	if len(v.Formats) > 0 {
		clone.Formats = append([]FormatInfo(nil), v.Formats...)
		for i := range clone.Formats {
			clone.Formats[i].Parts = append([]string(nil), v.Formats[i].Parts...)
		}
	}
	return &clone
}

func (c *Client) emitExtractionEvent(stage, phase, source, detail string) {
	if c == nil || c.config.OnExtractionEvent == nil {
		return
	}
	c.config.OnExtractionEvent(ExtractionEvent{
		Stage:  stage,
		Phase:  phase,
		Client: source,
		Detail: detail,
	})
}

// Close releases source-owned streaming servers. Caller-supplied transports remain owned by the caller.
func (c *Client) Close() error {
	var result error
	for _, src := range c.sources {
		if closer, ok := src.(interface{ Close() error }); ok {
			result = errors.Join(result, closer.Close())
		}
	}
	return result
}
