package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// StreamOptions controls format selection for stream-first APIs.
type StreamOptions struct {
	Itag int
	Mode SelectionMode
}

// OpenStream resolves and opens a readable stream without writing a local file.
// Returned FormatInfo describes the selected stream format.
func (c *Client) OpenStream(ctx context.Context, input string, options StreamOptions) (io.ReadCloser, FormatInfo, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	videoID, err := normalizeVideoID(input)
	if err != nil {
		return nil, FormatInfo{}, err
	}

	formats, err := c.GetFormats(ctx, videoID)
	if err != nil {
		return nil, FormatInfo{}, err
	}
	if len(formats) == 0 {
		return nil, FormatInfo{}, ErrNoPlayableFormats
	}
	filteredFormats, skipReasons := filterFormatsByPoTokenPolicy(formats, c.config)
	if len(filteredFormats) == 0 && len(skipReasons) > 0 {
		for _, skip := range skipReasons {
			c.warnf("format skipped by po token policy: itag=%d protocol=%s reason=%s", skip.Itag, skip.Protocol, skip.Reason)
		}
		return nil, FormatInfo{}, &NoPlayableFormatsDetailError{
			Mode:  normalizeSelectionMode(options.Mode),
			Skips: skipReasons,
		}
	}
	if len(filteredFormats) > 0 {
		formats = filteredFormats
	}

	chosen, ok := selectDownloadFormat(formats, DownloadOptions{
		Itag: options.Itag,
		Mode: options.Mode,
	})
	if !ok {
		return nil, FormatInfo{}, fmt.Errorf("%w: itag=%d mode=%s", ErrNoPlayableFormats, options.Itag, normalizeSelectionMode(options.Mode))
	}

	streamURL, err := c.resolveSelectedFormatURL(ctx, videoID, chosen)
	if err != nil {
		return nil, FormatInfo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return nil, FormatInfo{}, err
	}
	applyMediaRequestHeaders(req, c.config.RequestHeaders, videoID)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, FormatInfo{}, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, FormatInfo{}, fmt.Errorf("stream open failed: status=%d", resp.StatusCode)
	}
	return resp.Body, chosen, nil
}

// OpenFormatStream opens the selected itag stream as io.ReadCloser.
func (c *Client) OpenFormatStream(ctx context.Context, input string, itag int) (io.ReadCloser, FormatInfo, error) {
	return c.OpenStream(ctx, input, StreamOptions{
		Itag: itag,
	})
}
