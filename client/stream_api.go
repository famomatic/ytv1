package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/famomatic/ytv1/internal/downloader"
	"github.com/famomatic/ytv1/internal/source"
)

// StreamOptions controls format selection for stream-first APIs.
type StreamOptions struct {
	Itag int
	Mode SelectionMode
}

// OpenStream resolves and opens a readable stream without writing a local file.
// Returned FormatInfo describes the selected stream format.
func (c *Client) OpenStream(ctx context.Context, input string, options StreamOptions) (io.ReadCloser, FormatInfo, error) {
	// Metadata requests (format list, URL resolution) are bounded by the
	// package RequestTimeout via metaCtx. The media GET request uses
	// streamCtx (cancel-only, no deadline) so the caller's read is not
	// aborted when RequestTimeout expires; a context deadline cannot be
	// deferred, and net/http cancels in-flight body reads at deadline.
	metaCtx, metaCancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer metaCancel()

	streamCtx, streamCancel := streamCtxForMediaRequest(ctx)

	// Non-YouTube sources (e.g. SOOP) are dispatched before the YouTube path.
	// Unlike GetVideo/Download which already do this, OpenStream was missing
	// source dispatch, so calling OpenStream with a SOOP URL would fail at
	// normalizeVideoID with "unsupported host".
	if src := c.matchSource(input); src != nil {
		return c.openStreamViaSource(ctx, src, input, options, streamCancel)
	}

	videoID, err := normalizeVideoID(input)
	if err != nil {
		streamCancel()
		return nil, FormatInfo{}, err
	}

	formats, err := c.GetFormats(metaCtx, videoID)
	if err != nil {
		streamCancel()
		return nil, FormatInfo{}, err
	}
	if len(formats) == 0 {
		streamCancel()
		return nil, FormatInfo{}, ErrNoPlayableFormats
	}
	filteredFormats, skipReasons := filterFormatsByPoTokenPolicy(formats, c.config)
	if len(filteredFormats) == 0 && len(skipReasons) > 0 {
		for _, skip := range skipReasons {
			c.warnf("format skipped by po token policy: itag=%d protocol=%s reason=%s", skip.Itag, skip.Protocol, skip.Reason)
		}
		streamCancel()
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
		streamCancel()
		return nil, FormatInfo{}, fmt.Errorf("%w: itag=%d mode=%s", ErrNoPlayableFormats, options.Itag, normalizeSelectionMode(options.Mode))
	}

	streamURL, err := c.resolveSelectedFormatURL(metaCtx, videoID, chosen)
	if err != nil {
		streamCancel()
		return nil, FormatInfo{}, err
	}
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, streamURL, nil)
	if err != nil {
		streamCancel()
		return nil, FormatInfo{}, err
	}
	applyMediaRequestHeadersForSourceClient(req, c.config.RequestHeaders, videoID, chosen.SourceClient)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		streamCancel()
		return nil, FormatInfo{}, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		streamCancel()
		return nil, FormatInfo{}, fmt.Errorf("stream open failed: status=%s", resp.Status)
	}
	// Hand the caller a body whose Close() cancels streamCtx, releasing the
	// underlying connection. streamCtx carries no deadline, so the caller
	// controls the read lifetime. streamCancel is NOT deferred here: doing so
	// would cancel streamCtx as soon as OpenStream returns, aborting the
	// caller's read. Each error path above calls streamCancel() explicitly;
	// the success path transfers ownership to streamBody.Close().
	return &streamBody{rc: resp.Body, cancel: streamCancel}, chosen, nil
}

// openStreamViaSource opens a readable stream from a non-YouTube source.
// SOOP VODs are HLS multi-part streams; we stream them via the HLS downloader
// piped through a pipe, so the caller gets a unified io.ReadCloser.
func (c *Client) openStreamViaSource(ctx context.Context, src source.Source, input string, options StreamOptions, streamCancel func()) (io.ReadCloser, FormatInfo, error) {
	// Reuse cached extraction if available (GetVideo may have already run).
	var info *VideoInfo
	if s, ok := c.getSession(sourceInputKey(src.Name(), input)); ok && s.Info != nil {
		info = cloneVideoInfo(s.Info)
	} else {
		media, err := src.Extract(ctx, input)
		if err != nil {
			streamCancel()
			return nil, FormatInfo{}, err
		}
		if media == nil || len(media.Formats) == 0 {
			streamCancel()
			return nil, FormatInfo{}, ErrNoPlayableFormats
		}
		info = mediaToVideoInfo(media)
		c.cacheSourceSession(src.Name(), input, info, media.MediaHeaders)
	}

	// Select audio-only format
	audioCandidates := make([]FormatInfo, 0, len(info.Formats))
	for _, f := range info.Formats {
		if f.HasAudio && !f.HasVideo {
			audioCandidates = append(audioCandidates, f)
		}
	}
	if len(audioCandidates) == 0 {
		// Fall back to any format with audio
		for _, f := range info.Formats {
			if f.HasAudio {
				audioCandidates = append(audioCandidates, f)
			}
		}
	}
	if len(audioCandidates) == 0 {
		streamCancel()
		return nil, FormatInfo{}, ErrNoPlayableFormats
	}

	chosen := audioCandidates[0]
	// Pick the highest bitrate for audio-only
	for _, f := range audioCandidates[1:] {
		if f.Bitrate > chosen.Bitrate {
			chosen = f
		}
	}

	// HLS stream: pipe through the HLS downloader
	pr, pw := io.Pipe()

	// Build media headers from source session
	headers := c.applySourceMediaHeaders(sourceSessionKey(src.Name(), info.ID), nil)

	playlistURLs := chosen.Parts
	if len(playlistURLs) == 0 {
		playlistURLs = []string{chosen.URL}
	}

	go func() {
		// streamCancel is owned by the caller; we close pw on exit which
		// signals EOF/error to the reader side.
		defer streamCancel()
		defer pw.Close()

		transport := downloader.TransportConfig{
			MaxRetries:               c.config.DownloadTransport.MaxRetries,
			InitialBackoff:           c.config.DownloadTransport.InitialBackoff,
			MaxBackoff:               c.config.DownloadTransport.MaxBackoff,
			RetryStatusCodes:         append([]int(nil), c.config.DownloadTransport.RetryStatusCodes...),
			SkipUnavailableFragments: c.config.DownloadTransport.SkipUnavailableFragments,
			MaxSkippedFragments:      c.config.DownloadTransport.MaxSkippedFragments,
		}

		var lastErr error
		for _, url := range playlistURLs {
			dl := downloader.NewHLSDownloader(c.config.HTTPClient, url).
				WithRequestHeaders(headers).
				WithTransportConfig(transport)
			if err := dl.Download(ctx, pw); err != nil {
				lastErr = err
				break
			}
		}
		if lastErr != nil {
			pw.CloseWithError(lastErr)
		}
	}()

	streamBody := &streamBody{rc: pr, cancel: func() {
		pr.Close()
		streamCancel()
	}}
	return streamBody, chosen, nil
}

// streamCtxForMediaRequest returns a cancel-only context derived from the
// caller's ctx with no deadline of its own. A context deadline cannot be
// deferred, and net/http aborts an in-flight body read once the request
// context deadline expires; using the caller's ctx directly for the media
// GET would surface context.DeadlineExceeded to the caller mid-read when
// RequestTimeout is short. The returned cancel func is intended to be wired
// into the streamBody closer so an abandoned read can still be torn down.
func streamCtxForMediaRequest(ctx context.Context) (context.Context, context.CancelFunc) {
	// Derive from the caller's ctx so the caller's own deadline/cancellation
	// still propagate, but never impose a new deadline here. RequestTimeout
	// is applied separately to metadata requests via metaCtx.
	return context.WithCancel(ctx)
}

// streamBody wraps an HTTP response body and ensures the request context's
// cancel func is invoked exactly once when the body is closed, so the
// underlying connection is released and any timeout context is finalized.
type streamBody struct {
	rc     io.ReadCloser
	cancel context.CancelFunc

	closeOnce sync.Once
	closed    atomic.Bool
}

func (s *streamBody) Read(p []byte) (int, error) {
	// Check the closed flag without holding a mutex. Close sets this flag
	// before closing the underlying body, so a concurrent or post-close Read
	// returns EOF instead of racing into rc.Read after rc.Close has run.
	if s.closed.Load() {
		return 0, io.EOF
	}
	return s.rc.Read(p)
}

func (s *streamBody) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
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
