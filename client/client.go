package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/mjmst/ytv1/internal/formats"
	"github.com/mjmst/ytv1/internal/innertube"
	"github.com/mjmst/ytv1/internal/orchestrator"
	"github.com/mjmst/ytv1/internal/playerjs"
	"github.com/mjmst/ytv1/internal/policy"
	"github.com/mjmst/ytv1/internal/types"
)

// Client is the high-level YouTube client.
type Client struct {
	config Config
	engine *orchestrator.Engine
	playerJSResolver playerjs.Resolver
	sessionsMu sync.RWMutex
	sessions   map[string]videoSession
}

type videoSession struct {
	Response  *innertube.PlayerResponse
	PlayerURL string
}

// New creates a new YouTube client.
func New(config Config) *Client {
	return NewClient(config)
}

// NewClient creates a new YouTube client.
func NewClient(config Config) *Client {
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}

	registry := innertube.NewRegistry()
	innerCfg := config.ToInnerTubeConfig()
	selector := policy.NewSelector(registry, innerCfg.ClientOverrides)
	engine := orchestrator.NewEngine(selector, innerCfg)
	jsResolver := playerjs.NewResolver(
		config.HTTPClient,
		playerjs.NewMemoryCache(),
		playerjs.ResolverConfig{
			BaseURL: innerCfg.PlayerJSBaseURL,
			UserAgent: innerCfg.PlayerJSUserAgent,
			Headers: innerCfg.PlayerJSHeaders,
		},
	)

	return &Client{
		config: config,
		engine: engine,
		playerJSResolver: jsResolver,
		sessions: make(map[string]videoSession),
	}
}

// GetVideo fetches video metadata and normalized formats for the input ID/URL.
func (c *Client) GetVideo(ctx context.Context, input string) (*VideoInfo, error) {
	videoID, err := normalizeVideoID(input)
	if err != nil {
		return nil, err
	}

	resp, err := c.engine.GetVideoInfo(ctx, videoID)
	if err != nil {
		return nil, mapError(err)
	}

	parsedFormats := formats.Parse(resp)
	formats.SortByBest(parsedFormats)

	outFormats := make([]FormatInfo, 0, len(parsedFormats))
	for _, f := range parsedFormats {
		outFormats = append(outFormats, toFormatInfo(f))
	}

	info := &VideoInfo{
		ID:      resp.VideoDetails.VideoID,
		Title:   resp.VideoDetails.Title,
		Author:  resp.VideoDetails.Author,
		Formats: outFormats,
		DashManifestURL: resp.StreamingData.DashManifestURL,
		HLSManifestURL:  resp.StreamingData.HlsManifestURL,
	}

	playerURL, _ := c.playerJSResolver.GetPlayerURL(ctx, videoID)
	info.DashManifestURL = c.resolveManifestURL(ctx, info.DashManifestURL, playerURL)
	info.HLSManifestURL = c.resolveManifestURL(ctx, info.HLSManifestURL, playerURL)
	c.sessionsMu.Lock()
	c.sessions[videoID] = videoSession{
		Response:  resp,
		PlayerURL: playerURL,
	}
	c.sessionsMu.Unlock()

	return info, nil
}

// GetFormats returns normalized formats only.
func (c *Client) GetFormats(ctx context.Context, input string) ([]FormatInfo, error) {
	v, err := c.GetVideo(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(v.Formats) == 0 {
		return nil, ErrNoPlayableFormats
	}
	return v.Formats, nil
}

// ResolveStreamURL resolves a direct playable URL for a specific itag.
func (c *Client) ResolveStreamURL(ctx context.Context, videoID string, itag int) (string, error) {
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
		return raw.URL, nil
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

	jsBody, err := c.playerJSResolver.GetPlayerJS(ctx, session.PlayerURL)
	if err != nil {
		return "", ErrChallengeNotSolved
	}
	decipherer := playerjs.NewDecipherer(jsBody)

	if s := params.Get("s"); s != "" {
		decSig, err := decipherer.DecipherSignature(s)
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
		decN, err := decipherer.DecipherN(n)
		if err != nil {
			return "", ErrChallengeNotSolved
		}
		q.Set("n", decN)
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}

func toFormatInfo(f formats.Format) FormatInfo {
	hasVideo := f.Width > 0 || f.Height > 0
	hasAudio := f.AudioChannels > 0 || f.AudioSampleRate > 0
	return FormatInfo{
		Itag:        f.Itag,
		URL:         f.URL,
		MimeType:    f.MimeType,
		HasAudio:    hasAudio,
		HasVideo:    hasVideo,
		Bitrate:     f.Bitrate,
		Width:       f.Width,
		Height:      f.Height,
		FPS:         f.FPS,
		Ciphered:    f.URL == "",
		Quality:     f.Quality,
		QualityLabel:f.QualityLabel,
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
		return ErrAllClientsFailed
	case errors.Is(err, types.ErrLoginRequired):
		return ErrLoginRequired
	case errors.Is(err, types.ErrVideoUnavailable):
		return ErrUnavailable
	case errors.Is(err, types.ErrAgeRestricted):
		return ErrUnavailable
	}

	var playabilityErr *orchestrator.PlayabilityError
	if errors.As(err, &playabilityErr) {
		if playabilityErr.RequiresLogin() {
			return ErrLoginRequired
		}
		if playabilityErr.IsAgeRestricted() {
			return ErrLoginRequired
		}
		return ErrUnavailable
	}

	var allFailedErr *orchestrator.AllClientsFailedError
	if errors.As(err, &allFailedErr) {
		hasUnavailable := false
		for _, attempt := range allFailedErr.Attempts {
			if !errors.As(attempt.Err, &playabilityErr) {
				continue
			}
			if playabilityErr.RequiresLogin() || playabilityErr.IsAgeRestricted() {
				return ErrLoginRequired
			}
			if playabilityErr.IsGeoRestricted() || playabilityErr.IsUnavailable() {
				hasUnavailable = true
			}
		}
		if hasUnavailable {
			return ErrUnavailable
		}
		return ErrAllClientsFailed
	}

	var httpStatusErr *orchestrator.HTTPStatusError
	if errors.As(err, &httpStatusErr) {
		return ErrAllClientsFailed
	}
	var poTokenErr *orchestrator.PoTokenRequiredError
	if errors.As(err, &poTokenErr) {
		return ErrAllClientsFailed
	}

	return err
}

func (c *Client) getSession(videoID string) (videoSession, bool) {
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()
	s, ok := c.sessions[videoID]
	return s, ok
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
	jsBody, err := c.playerJSResolver.GetPlayerJS(ctx, playerURL)
	if err != nil {
		return manifestURL
	}
	decipherer := playerjs.NewDecipherer(jsBody)
	rewritten, err := rewriteURLParam(manifestURL, "n", decipherer.DecipherN)
	if err != nil {
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
