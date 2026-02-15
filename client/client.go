package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	selector := policy.NewSelector(registry)
	innerCfg := config.ToInnerTubeConfig()
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
	}

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
	formats, err := c.GetFormats(ctx, videoID)
	if err != nil {
		return "", err
	}

	for _, f := range formats {
		if f.Itag != itag {
			continue
		}
		if f.URL == "" {
			return "", ErrChallengeNotSolved
		}
		return f.URL, nil
	}

	return "", fmt.Errorf("%w: itag=%d", ErrNoPlayableFormats, itag)
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

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unplayable"):
		if strings.Contains(msg, "login") || strings.Contains(msg, "sign in") {
			return ErrLoginRequired
		}
		return ErrUnavailable
	case strings.Contains(msg, "innertube error"):
		return ErrAllClientsFailed
	}
	return err
}
