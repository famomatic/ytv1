package client

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
)

// StreamOptions controls format selection for stream-first APIs.
type StreamOptions struct {
	Itag int
	Mode SelectionMode
}

// OpenStream resolves and opens a readable stream without writing a local file.
// Returned FormatInfo describes the selected stream format.
func (c *Client) OpenStream(ctx context.Context, input string, options StreamOptions) (io.ReadCloser, FormatInfo, error) {
	metaCtx, metaCancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer metaCancel()
	streamCtx, streamCancel := streamCtxForMediaRequest(ctx)
	fail := func(err error) (io.ReadCloser, FormatInfo, error) { streamCancel(); return nil, FormatInfo{}, err }
	var info *VideoInfo
	var id string
	var err error
	if src := c.matchSource(input); src != nil {
		if sess, ok := c.getSession(sourceInputKey(src.Name(), input)); ok {
			info = cloneVideoInfo(sess.Info)
		}
		if info == nil {
			info, err = c.getVideoViaSource(metaCtx, src, input)
		}
		if err != nil {
			return fail(err)
		}
		id = sourceSessionKey(src.Name(), info.ID)
	} else {
		info, err = c.GetVideo(metaCtx, input)
		if err != nil {
			return fail(err)
		}
		id = info.ID
	}
	formats, skips := filterFormatsByPoTokenPolicy(info.Formats, c.config)
	if len(formats) == 0 {
		return fail(&NoPlayableFormatsDetailError{Mode: normalizeSelectionMode(options.Mode), Skips: skips})
	}
	selected, err := SelectFormatsForDownloadOptions(formats, DownloadOptions{Itag: options.Itag, Mode: options.Mode})
	if err != nil {
		return fail(err)
	}
	if video, audio, ok := hlsMergeFormats(selected); ok {
		pr, pw := io.Pipe()
		go func() {
			defer streamCancel()
			err := c.muxHLSStreamsTo(streamCtx, id, video, audio, pw, "-")
			_ = pw.CloseWithError(err)
		}()
		return &streamBody{rc: pr, cancel: streamCancel}, mergedHLSOutputFormat(video, audio), nil
	}
	chosen := selected[0]
	if len(selected) > 1 {
		var ok bool
		chosen, ok = selectDownloadFormat(formats, DownloadOptions{Itag: options.Itag, Mode: options.Mode})
		if !ok {
			return fail(ErrNoPlayableFormats)
		}
	}
	raw, err := c.resolveSelectedFormatURL(metaCtx, id, chosen)
	if err != nil {
		return fail(err)
	}
	body, err := c.openFormatMedia(streamCtx, id, chosen, raw)
	if err != nil {
		return fail(err)
	}
	return &streamBody{rc: body, cancel: streamCancel}, chosen, nil
}

func mergedHLSOutputFormat(video, audio FormatInfo) FormatInfo {
	output := video
	output.Protocol = "mpegts"
	output.MimeType = "video/mp2t"
	output.HasAudio = true
	output.HasVideo = true
	output.Bitrate += audio.Bitrate
	output.ContentLength = 0
	output.Parts = nil
	return output
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
	closeErr  error
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
		s.closeErr = s.rc.Close()
	})
	return s.closeErr
}

// OpenFormatStream opens the selected itag stream as io.ReadCloser.
func (c *Client) OpenFormatStream(ctx context.Context, input string, itag int) (io.ReadCloser, FormatInfo, error) {
	return c.OpenStream(ctx, input, StreamOptions{
		Itag: itag,
	})
}
