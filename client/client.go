package client

import (
	"context"
	"net/http"

	"github.com/mjmst/ytv1/internal/formats"
	"github.com/mjmst/ytv1/internal/innertube"
	"github.com/mjmst/ytv1/internal/orchestrator"
	"github.com/mjmst/ytv1/internal/policy"
)

// Client is the high-level YouTube client.
type Client struct {
	config Config
	engine *orchestrator.Engine
}

// NewClient creates a new YouTube client.
func NewClient(config Config) *Client {
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}

	registry := innertube.NewRegistry()
	selector := policy.NewSelector(registry)
	engine := orchestrator.NewEngine(selector, config.ToInnerTubeConfig())

	return &Client{
		config: config,
		engine: engine,
	}
}

// VideoInfo contains the extracted video information.
type VideoInfo struct {
	ID      string
	Title   string
	Formats []formats.Format
}

// GetVideo fetches video information for the given video ID.
func (c *Client) GetVideo(ctx context.Context, videoID string) (*VideoInfo, error) {
	resp, err := c.engine.GetVideoInfo(ctx, videoID)
	if err != nil {
		return nil, err
	}

	parsedFormats := formats.Parse(resp)
	formats.SortByBest(parsedFormats)

	info := &VideoInfo{
		ID:      resp.VideoDetails.VideoID,
		Title:   resp.VideoDetails.Title,
		Formats: parsedFormats,
	}

	return info, nil
}
