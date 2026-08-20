package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// LiveAdaptiveDownloader downloads completed ("post-live") adaptive formats
// whose media is addressed as sq=<n> fragments. It mirrors yt-dlp's
// _live_adaptive_fragments post-live path: the last sequence number is read
// from the X-Head-Seqnum response header on the bare URL, then fragments
// sq=0..lastSeq-2 are fetched sequentially. The bare URL itself serves no
// data blocks for ended streams, so fragment enumeration is the only way to
// obtain the complete media.
type LiveAdaptiveDownloader struct {
	Client    *http.Client
	BaseURL   string
	Headers   http.Header
	Transport TransportConfig
}

func NewLiveAdaptiveDownloader(client *http.Client, baseURL string) *LiveAdaptiveDownloader {
	return &LiveAdaptiveDownloader{
		Client:  client,
		BaseURL: baseURL,
	}
}

func (d *LiveAdaptiveDownloader) WithRequestHeaders(headers http.Header) *LiveAdaptiveDownloader {
	d.Headers = cloneHeader(headers)
	return d
}

func (d *LiveAdaptiveDownloader) WithTransportConfig(cfg TransportConfig) *LiveAdaptiveDownloader {
	d.Transport = cfg
	return d
}

func (d *LiveAdaptiveDownloader) Download(ctx context.Context, w io.Writer) error {
	lastSeq, err := d.lastSequence(ctx)
	if err != nil {
		return err
	}
	// Upstream post-live math: last_seq = header + 1, then -2, and fragments
	// run over range(0, last_seq). The final two segments are excluded.
	end := lastSeq - 1
	for seq := 0; seq < end; seq++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.downloadFragment(ctx, seq, w); err != nil {
			return fmt.Errorf("live adaptive fragment sq=%d: %w", seq, err)
		}
	}
	return nil
}

func (d *LiveAdaptiveDownloader) lastSequence(ctx context.Context) (int, error) {
	effective := normalizeTransportConfig(d.Transport)
	var lastErr error
	for attempt := 0; attempt <= effective.MaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.BaseURL, nil)
		if err != nil {
			return -1, err
		}
		applyRequestHeaders(req, d.Headers)
		resp, err := d.Client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			// The bare URL may return an empty or streaming body; only the
			// headers matter here, so drain a bounded amount and close.
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if seq, convErr := strconv.Atoi(resp.Header.Get("X-Head-Seqnum")); convErr == nil && seq >= 0 {
				return seq, nil
			}
			lastErr = fmt.Errorf("live adaptive: X-Head-Seqnum header missing or invalid")
		}
		if !isRetryableError(lastErr, effective) || attempt == effective.MaxRetries {
			return -1, lastErr
		}
		if err := waitBackoff(ctx, effective.backoffFor(attempt)); err != nil {
			return -1, err
		}
	}
	return -1, lastErr
}

func (d *LiveAdaptiveDownloader) downloadFragment(ctx context.Context, seq int, w io.Writer) error {
	fragURL, err := withSeqQuery(d.BaseURL, seq)
	if err != nil {
		return err
	}
	body, err := doGETBytesWithRetry(ctx, d.Client, fragURL, d.Headers, d.Transport)
	if err != nil {
		if shouldSkipFragmentError(err, d.Transport) {
			return nil
		}
		return err
	}
	_, err = w.Write(body)
	return err
}

func withSeqQuery(rawURL string, seq int) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("sq", strconv.Itoa(seq))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
