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
	"time"

	"github.com/famomatic/ytv1/internal/formats"
	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/orchestrator"
	"github.com/famomatic/ytv1/internal/playerjs"
	"github.com/famomatic/ytv1/internal/policy"
	"github.com/famomatic/ytv1/internal/types"
)

// Client is the high-level YouTube client.
type Client struct {
	config           Config
	engine           *orchestrator.Engine
	playerJSResolver playerjs.Resolver
	logger           Logger
	sessionsMu       sync.RWMutex
	sessions         map[string]videoSession
	challengesMu     sync.RWMutex
	challenges       map[string]challengeSolutions
}

type videoSession struct {
	Response   *innertube.PlayerResponse
	PlayerURL  string
	Info       *VideoInfo
	CachedAt   time.Time
	LastAccess time.Time
}

// New creates a new YouTube client.
func New(config Config) *Client {
	return NewClient(config)
}

// NewClient creates a new YouTube client.
func NewClient(config Config) *Client {
	if config.HTTPClient == nil {
		config.HTTPClient = defaultHTTPClient(config.ProxyURL)
	}
	if config.CookieJar != nil {
		config.HTTPClient.Jar = config.CookieJar
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
		sessions:         make(map[string]videoSession),
		challenges:       make(map[string]challengeSolutions),
	}
}

// GetVideo fetches video metadata and normalized formats for the input ID/URL.
func (c *Client) GetVideo(ctx context.Context, input string) (*VideoInfo, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	videoID, err := normalizeVideoID(input)
	if err != nil {
		return nil, err
	}

	resp, err := c.engine.GetVideoInfo(ctx, videoID)
	if err != nil {
		return nil, mapError(err)
	}

	parsedFormats := formats.Parse(resp)

	outFormats := make([]FormatInfo, 0, len(parsedFormats))
	for _, f := range parsedFormats {
		outFormats = append(outFormats, toFormatInfo(f))
	}

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
		Formats:         outFormats,
		DashManifestURL: resp.StreamingData.DashManifestURL,
		HLSManifestURL:  resp.StreamingData.HlsManifestURL,
	}

	c.emitExtractionEvent("webpage", "start", "web", videoID)
	playerURL, playerErr := c.playerJSResolver.GetPlayerURL(ctx, videoID)
	if playerErr != nil {
		c.emitExtractionEvent("webpage", "failure", "web", playerErr.Error())
	} else {
		c.emitExtractionEvent("webpage", "success", "web", playerURL)
	}
	c.primeChallengeSolutions(ctx, playerURL, resp, info.DashManifestURL, info.HLSManifestURL)
	info.DashManifestURL = c.resolveManifestURL(ctx, info.DashManifestURL, playerURL)
	info.HLSManifestURL = c.resolveManifestURL(ctx, info.HLSManifestURL, playerURL)

	manifestFormats := c.loadManifestFormats(ctx, info.DashManifestURL, info.HLSManifestURL)
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
	manifestURL := c.resolveManifestURL(ctx, session.Response.StreamingData.DashManifestURL, session.PlayerURL)
	if manifestURL == "" {
		return "", fmt.Errorf("%w: dash manifest unavailable for video=%s", ErrNoPlayableFormats, videoID)
	}
	manifest, err := formats.FetchDASHManifest(ctx, c.config.HTTPClient, manifestURL)
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
	manifestURL := c.resolveManifestURL(ctx, session.Response.StreamingData.HlsManifestURL, session.PlayerURL)
	if manifestURL == "" {
		return "", fmt.Errorf("%w: hls manifest unavailable for video=%s", ErrNoPlayableFormats, videoID)
	}
	manifest, err := formats.FetchHLSManifest(ctx, c.config.HTTPClient, manifestURL)
	if err != nil {
		return "", err
	}
	return manifest.RawContent, nil
}

// ResolveStreamURL resolves a direct playable URL for a specific itag.
func (c *Client) ResolveStreamURL(ctx context.Context, videoID string, itag int) (string, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	videoID, err := normalizeVideoID(videoID)
	if err != nil {
		return "", err
	}

	session, ok := c.getSession(videoID)
	if !ok {
		if _, err := c.GetVideo(ctx, videoID); err != nil {
			return "", err
		}
		session, ok = c.getSession(videoID)
		if !ok {
			return "", ErrChallengeNotSolved
		}
	}

	raw, found := findRawFormat(session.Response, itag)
	if !found {
		return "", fmt.Errorf("%w: itag=%d", ErrNoPlayableFormats, itag)
	}

	if raw.URL != "" {
		rewritten, err := c.resolveDirectURL(ctx, raw.URL, session.PlayerURL)
		if err != nil {
			return "", err
		}
		return rewritten, nil
	}

	cipher := raw.SignatureCipher
	if cipher == "" {
		cipher = raw.Cipher
	}
	if cipher == "" || session.PlayerURL == "" {
		return "", ErrChallengeNotSolved
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

	return u.String(), nil
}

func (c *Client) resolveSelectedFormatURL(ctx context.Context, videoID string, f FormatInfo) (string, error) {
	videoID, err := normalizeVideoID(videoID)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(f.URL) != "" {
		session, ok := c.getSession(videoID)
		if !ok {
			if _, err := c.GetVideo(ctx, videoID); err != nil {
				return "", err
			}
			session, ok = c.getSession(videoID)
			if !ok {
				return "", ErrChallengeNotSolved
			}
		}
		return c.resolveDirectURL(ctx, f.URL, session.PlayerURL)
	}

	return c.ResolveStreamURL(ctx, videoID, f.Itag)
}

func toFormatInfo(f formats.Format) FormatInfo {
	hasVideo := f.HasVideo
	hasAudio := f.HasAudio
	return FormatInfo{
		Itag:         f.Itag,
		URL:          f.URL,
		MimeType:     f.MimeType,
		Protocol:     f.Protocol,
		HasAudio:     hasAudio,
		HasVideo:     hasVideo,
		Bitrate:      f.Bitrate,
		Width:        f.Width,
		Height:       f.Height,
		FPS:          f.FPS,
		Ciphered:     f.Ciphered,
		Quality:      f.Quality,
		QualityLabel: f.QualityLabel,
		SourceClient: f.SourceClient,
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
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	s, ok := c.sessions[videoID]
	if !ok {
		return videoSession{}, false
	}
	now := time.Now()
	if ttl := c.config.SessionCacheTTL; ttl > 0 && !s.CachedAt.IsZero() && now.Sub(s.CachedAt) > ttl {
		delete(c.sessions, videoID)
		return videoSession{}, false
	}
	s.LastAccess = now
	c.sessions[videoID] = s
	return s, ok
}

func (c *Client) putSession(videoID string, session videoSession) {
	now := time.Now()
	if session.CachedAt.IsZero() {
		session.CachedAt = now
	}
	session.LastAccess = now

	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	if c.sessions == nil {
		c.sessions = make(map[string]videoSession)
	}
	c.evictExpiredLocked(now)
	c.sessions[videoID] = session
	c.evictLRULocked()
}

func (c *Client) evictExpiredLocked(now time.Time) {
	ttl := c.config.SessionCacheTTL
	if ttl <= 0 {
		return
	}
	for id, session := range c.sessions {
		if session.CachedAt.IsZero() {
			continue
		}
		if now.Sub(session.CachedAt) > ttl {
			delete(c.sessions, id)
		}
	}
}

func (c *Client) evictLRULocked() {
	maxEntries := c.config.SessionCacheMaxEntries
	if maxEntries <= 0 {
		return
	}
	for len(c.sessions) > maxEntries {
		var oldestID string
		var oldest time.Time
		first := true
		for id, session := range c.sessions {
			candidate := session.LastAccess
			if candidate.IsZero() {
				candidate = session.CachedAt
			}
			if first || candidate.Before(oldest) {
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
	session, ok := c.getSession(videoID)
	if ok {
		return session, videoID, nil
	}
	if _, err := c.GetVideo(ctx, videoID); err != nil {
		return videoSession{}, "", err
	}
	session, ok = c.getSession(videoID)
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

func (c *Client) resolveManifestURL(ctx context.Context, manifestURL, playerURL string) string {
	if manifestURL == "" || playerURL == "" || !hasQueryParam(manifestURL, "n") {
		return manifestURL
	}
	rewritten, err := rewriteURLParam(manifestURL, "n", func(value string) (string, error) {
		return c.decodeNWithCache(ctx, playerURL, value)
	})
	if err != nil {
		c.warnf("n challenge decode failed for manifest url; using original url: %v", err)
		return manifestURL
	}
	return rewritten
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
		if dash, err := formats.FetchDASHManifest(ctx, c.httpClient(), dashURL); err == nil {
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
		if hls, err := formats.FetchHLSManifest(ctx, c.httpClient(), hlsURL); err == nil {
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

func (c *Client) resolveDirectURL(ctx context.Context, rawURL string, playerURL string) (string, error) {
	if rawURL == "" {
		return "", ErrChallengeNotSolved
	}
	if !hasQueryParam(rawURL, "n") {
		return rawURL, nil
	}
	if playerURL == "" {
		return "", ErrChallengeNotSolved
	}

	rewritten, err := rewriteURLParam(rawURL, "n", func(value string) (string, error) {
		return c.decodeNWithCache(ctx, playerURL, value)
	})
	if err != nil {
		c.warnf("n challenge decode failed for direct url; using original url: %v", err)
		return rawURL, nil
	}
	return rewritten, nil
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
