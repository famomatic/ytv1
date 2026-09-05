package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func mediaProtocol(f FormatInfo, raw string) string {
	if f.ManifestURL != "" {
		return "dash"
	}
	p := strings.ToLower(f.Protocol)
	if p == "hls" || p == "dash" {
		return p
	}
	u, err := url.Parse(raw)
	if err == nil {
		switch strings.ToLower(filepath.Ext(u.Path)) {
		case ".m3u8":
			return "hls"
		case ".mpd":
			return "dash"
		}
	}
	if f.TargetDurationSec > 0 && !f.ThisIsLive {
		return "live-adaptive"
	}
	return "https"
}

// openFormatMedia opens media bytes, regardless of how its URL addresses them.
// Segmented transports own their pipe and inherit cancellation from the reader.
func (c *Client) openFormatMedia(ctx context.Context, videoID string, f FormatInfo, raw string) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(ctx)
	if mediaProtocol(f, raw) != "https" {
		pr, pw := io.Pipe()
		go func() {
			defer cancel()
			err := c.writeFormatMedia(ctx, videoID, f, raw, pw)
			_ = pw.CloseWithError(err)
		}()
		return &streamBody{rc: pr, cancel: cancel}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	applyRequestHeaders(req, c.applySourceMediaHeaders(videoID, buildMediaRequestHeadersForSourceClient(c.config.RequestHeaders, videoID, f.SourceClient)))
	resp, err := c.httpClient().Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, &downloadHTTPStatusError{StatusCode: resp.StatusCode}
	}
	return &streamBody{rc: resp.Body, cancel: cancel}, nil
}

func (c *Client) writeFormatMedia(ctx context.Context, id string, f FormatInfo, raw string, w io.Writer) error {
	switch mediaProtocol(f, raw) {
	case "hls":
		parts := f.Parts
		if len(parts) == 0 {
			parts = []string{raw}
		}
		return c.hlsStreamTo(ctx, id, parts, w, "-", f)
	case "dash":
		return c.dashStreamTo(ctx, id, raw, w, "-", f)
	case "live-adaptive":
		return c.liveAdaptiveStreamTo(ctx, id, raw, w, "-", f)
	default:
		return c.streamToWriter(ctx, id, raw, f, w)
	}
}

func (c *Client) writeMediaOutput(ctx context.Context, path string, part bool, write func(io.Writer) error) (int64, error) {
	if path == "-" {
		w := &countingWriter{w: os.Stdout}
		err := write(w)
		return w.n, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return 0, err
	}
	target := path
	if part {
		target += ".part"
	}
	var f *os.File
	if err := retryFileAccess(ctx, c.config.DownloadTransport, func() error { var e error; f, e = createFile(target); return e }); err != nil {
		return 0, err
	}
	w := &countingWriter{w: f}
	err := write(w)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return w.n, err
	}
	if part {
		if err := retryFileAccess(ctx, c.config.DownloadTransport, func() error { return renameFile(target, path) }); err != nil {
			return w.n, fmt.Errorf("finalize media: %w", err)
		}
	}
	return w.n, nil
}
