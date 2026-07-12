package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
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

	videoID, err := normalizeVideoID(input)
	if err != nil {
		cancel()
		return nil, FormatInfo{}, err
	}

	formats, err := c.GetFormats(ctx, videoID)
	if err != nil {
		cancel()
		return nil, FormatInfo{}, err
	}
	if len(formats) == 0 {
		cancel()
		return nil, FormatInfo{}, ErrNoPlayableFormats
	}
	filteredFormats, skipReasons := filterFormatsByPoTokenPolicy(formats, c.config)
	if len(filteredFormats) == 0 && len(skipReasons) > 0 {
		for _, skip := range skipReasons {
			c.warnf("format skipped by po token policy: itag=%d protocol=%s reason=%s", skip.Itag, skip.Protocol, skip.Reason)
		}
		cancel()
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
		cancel()
		return nil, FormatInfo{}, fmt.Errorf("%w: itag=%d mode=%s", ErrNoPlayableFormats, options.Itag, normalizeSelectionMode(options.Mode))
	}

	streamURL, err := c.resolveSelectedFormatURL(ctx, videoID, chosen)
	if err != nil {
		cancel()
		return nil, FormatInfo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		cancel()
		return nil, FormatInfo{}, err
	}
	applyMediaRequestHeadersForSourceClient(req, c.config.RequestHeaders, videoID, chosen.SourceClient)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		cancel()
		return nil, FormatInfo{}, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		cancel()
		return nil, FormatInfo{}, fmt.Errorf("stream open failed: status=%s", resp.Status)
	}
	// Wrap the body so the request context (and its timeout/cancel) is only
	// torn down once the caller has finished reading the stream. Returning
	// resp.Body directly with `defer cancel()` would cancel the context as
	// soon as OpenStream returns, aborting the caller's read of the body.
	return &streamBody{rc: resp.Body, cancel: cancel}, chosen, nil
}

// streamBody wraps an HTTP response body and ensures the request context's
// cancel func is invoked exactly once when the body is closed, so the
// underlying connection is released and any timeout context is finalized.
type streamBody struct {
	rc     io.ReadCloser
	cancel context.CancelFunc

	once   sync.Once
	closed bool
	mu     sync.Mutex
}

func (s *streamBody) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, io.EOF
	}
	s.mu.Unlock()
	return s.rc.Read(p)
}

func (s *streamBody) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if s.cancel != nil {
			s.cancel()
		}
	})
	return s.rc.Close()
}

// OpenFormatStream opens the selected itag stream as io.ReadCloser.
func (c *Client) OpenFormatStream(ctx context.Context, input string, itag int) (io.ReadCloser, FormatInfo, error) {
	return c.OpenStream(ctx, input, StreamOptions{
		Itag: itag,
	})
}
