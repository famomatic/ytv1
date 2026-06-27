package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/famomatic/ytv1/internal/iox"
)

// TransportConfig controls retry/backoff behavior for downloader HTTP requests.
type TransportConfig struct {
	MaxRetries                  int
	InitialBackoff              time.Duration
	MaxBackoff                  time.Duration
	RetryStatusCodes            []int
	MaxConcurrency              int
	SkipUnavailableFragments    bool
	MaxSkippedFragments         int
	ThrottledRateBytesPerSecond int64
	ThrottledRateMinDuration    time.Duration
}

type effectiveTransportConfig struct {
	MaxRetries                  int
	InitialBackoff              time.Duration
	MaxBackoff                  time.Duration
	RetryStatusCodes            []int
	MaxConcurrency              int
	SkipUnavailableFragments    bool
	MaxSkippedFragments         int
	ThrottledRateBytesPerSecond int64
	ThrottledRateMinDuration    time.Duration
}

type downloadHTTPStatusError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *downloadHTTPStatusError) Error() string {
	return fmt.Sprintf("download failed: status=%d", e.StatusCode)
}

var errThrottledDownload = errors.New("download speed below throttled-rate threshold")

func normalizeTransportConfig(cfg TransportConfig) effectiveTransportConfig {
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	initialBackoff := cfg.InitialBackoff
	if initialBackoff <= 0 {
		initialBackoff = 500 * time.Millisecond
	}
	maxBackoff := cfg.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 3 * time.Second
	}
	statusCodes := cfg.RetryStatusCodes
	if len(statusCodes) == 0 {
		statusCodes = []int{
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		}
	}
	throttledRate := cfg.ThrottledRateBytesPerSecond
	if throttledRate < 0 {
		throttledRate = 0
	}
	throttledDuration := cfg.ThrottledRateMinDuration
	if throttledDuration <= 0 {
		throttledDuration = 3 * time.Second
	}
	return effectiveTransportConfig{
		MaxRetries:                  maxRetries,
		InitialBackoff:              initialBackoff,
		MaxBackoff:                  maxBackoff,
		RetryStatusCodes:            statusCodes,
		MaxConcurrency:              max(1, cfg.MaxConcurrency),
		SkipUnavailableFragments:    cfg.SkipUnavailableFragments,
		MaxSkippedFragments:         cfg.MaxSkippedFragments,
		ThrottledRateBytesPerSecond: throttledRate,
		ThrottledRateMinDuration:    throttledDuration,
	}
}

func (c effectiveTransportConfig) backoffFor(attempt int) time.Duration {
	backoff := c.InitialBackoff
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff > c.MaxBackoff {
			return c.MaxBackoff
		}
	}
	return backoff
}

func isRetryableError(err error, cfg effectiveTransportConfig) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, errThrottledDownload) {
		return true
	}
	var statusErr *downloadHTTPStatusError
	if errors.As(err, &statusErr) {
		for _, code := range cfg.RetryStatusCodes {
			if statusErr.StatusCode == code {
				return true
			}
		}
		return false
	}
	return true
}

func shouldSkipFragmentError(err error, cfg TransportConfig) bool {
	effective := normalizeTransportConfig(cfg)
	if !effective.SkipUnavailableFragments {
		return false
	}
	var statusErr *downloadHTTPStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.StatusCode == http.StatusNotFound || statusErr.StatusCode == http.StatusGone
}

func waitBackoff(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func doGETBytesWithRetry(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	headers http.Header,
	cfg TransportConfig,
) ([]byte, error) {
	effectiveCfg := normalizeTransportConfig(cfg)
	var lastErr error
	for attempt := 0; attempt <= effectiveCfg.MaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		applyRequestHeaders(req, headers)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := func() ([]byte, error) {
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return nil, &downloadHTTPStatusError{
						StatusCode: resp.StatusCode,
						RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
					}
				}
				return readAllWithTransportConfig(ctx, resp.Body, effectiveCfg)
			}()
			if readErr == nil {
				return body, nil
			}
			lastErr = readErr
		}
		if !isRetryableError(lastErr, effectiveCfg) || attempt == effectiveCfg.MaxRetries {
			return nil, lastErr
		}
		backoff := effectiveCfg.backoffFor(attempt)
		var statusErr *downloadHTTPStatusError
		if errors.As(lastErr, &statusErr) && statusErr.RetryAfter > backoff {
			backoff = statusErr.RetryAfter
		}
		if err := waitBackoff(ctx, backoff); err != nil {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed with unknown retry error")
}

func readAllWithTransportConfig(ctx context.Context, r io.Reader, cfg effectiveTransportConfig) ([]byte, error) {
	if cfg.ThrottledRateBytesPerSecond > 0 {
		r = throttledRateReader(ctx, r, cfg.ThrottledRateBytesPerSecond, cfg.ThrottledRateMinDuration)
	}
	return iox.ReadAllLimit(r, 100<<20) // 100 MB segment limit
}

func throttledRateReader(ctx context.Context, src io.Reader, bytesPerSecond int64, minDuration time.Duration) io.Reader {
	if bytesPerSecond <= 0 {
		return src
	}
	if minDuration <= 0 {
		minDuration = 3 * time.Second
	}
	return &throttleDetectReader{
		ctx:            ctx,
		src:            src,
		bytesPerSecond: bytesPerSecond,
		minDuration:    minDuration,
		start:          time.Now(),
	}
}

type throttleDetectReader struct {
	ctx            context.Context
	src            io.Reader
	bytesPerSecond int64
	minDuration    time.Duration
	start          time.Time
	read           int64
	throttleStart  time.Time
}

func (r *throttleDetectReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.read += int64(n)
		elapsed := time.Since(r.start)
		if elapsed > 0 {
			speed := float64(r.read) / elapsed.Seconds()
			if speed < float64(r.bytesPerSecond) {
				if r.throttleStart.IsZero() {
					r.throttleStart = time.Now()
				} else if time.Since(r.throttleStart) >= r.minDuration {
					return n, errThrottledDownload
				}
			} else {
				r.throttleStart = time.Time{}
			}
		}
	}
	if err == nil {
		select {
		case <-r.ctx.Done():
			return n, r.ctx.Err()
		default:
		}
	}
	return n, err
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		d := time.Until(when)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
