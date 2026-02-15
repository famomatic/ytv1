package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DownloadOptions controls stream download behavior.
type DownloadOptions struct {
	Itag       int
	Mode       SelectionMode
	OutputPath string
	Resume     bool
}

// DownloadResult describes a completed file download.
type DownloadResult struct {
	VideoID    string
	Itag       int
	OutputPath string
	Bytes      int64
}

// Download resolves the selected stream URL and writes it to a local file.
// If options.Itag is 0, format selection follows options.Mode (default: best).
// If options.OutputPath is empty, "<videoID>-<itag><ext>" is used.
func (c *Client) Download(ctx context.Context, input string, options DownloadOptions) (*DownloadResult, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	mode := normalizeSelectionMode(options.Mode)
	videoID, err := normalizeVideoID(input)
	if err != nil {
		return nil, err
	}

	formats, err := c.GetFormats(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if len(formats) == 0 {
		return nil, ErrNoPlayableFormats
	}
	filteredFormats, skipReasons := filterFormatsByPoTokenPolicy(formats, c.config)
	if len(filteredFormats) == 0 && len(skipReasons) > 0 {
		return nil, &NoPlayableFormatsDetailError{
			Mode:  mode,
			Skips: skipReasons,
		}
	}
	if len(filteredFormats) > 0 {
		formats = filteredFormats
	}

	chosen, ok := selectDownloadFormat(formats, options)
	if !ok {
		if options.Itag != 0 {
			return nil, fmt.Errorf("%w: itag=%d", ErrNoPlayableFormats, options.Itag)
		}
		if len(skipReasons) > 0 {
			return nil, &NoPlayableFormatsDetailError{
				Mode:  mode,
				Skips: skipReasons,
			}
		}
		return nil, fmt.Errorf("%w: mode=%s", ErrNoPlayableFormats, mode)
	}

	if mode == SelectionModeMP3 && c.config.MP3Transcoder == nil {
		return nil, &MP3TranscoderError{Mode: mode}
	}

	streamURL, err := c.ResolveStreamURL(ctx, videoID, chosen.Itag)
	if err != nil {
		return nil, err
	}

	outputPath := options.OutputPath
	if outputPath == "" {
		outputPath = defaultOutputPath(videoID, chosen.Itag, chosen.MimeType, mode)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil && filepath.Dir(outputPath) != "." {
		return nil, err
	}

	var written int64
	if mode == SelectionModeMP3 {
		out, err := os.Create(outputPath)
		if err != nil {
			return nil, err
		}
		defer out.Close()
		written, err = transcodeURLToMP3(ctx, c.config.HTTPClient, c.config.MP3Transcoder, streamURL, MP3TranscodeMetadata{
			VideoID:        videoID,
			SourceItag:     chosen.Itag,
			SourceMimeType: chosen.MimeType,
		}, out)
	} else {
		written, err = downloadURLToPath(ctx, c.config.HTTPClient, streamURL, outputPath, options.Resume, c.config.DownloadTransport)
	}
	if err != nil {
		return nil, err
	}

	return &DownloadResult{
		VideoID:    videoID,
		Itag:       chosen.Itag,
		OutputPath: outputPath,
		Bytes:      written,
	}, nil
}

func transcodeURLToMP3(
	ctx context.Context,
	httpClient *http.Client,
	transcoder MP3Transcoder,
	streamURL string,
	meta MP3TranscodeMetadata,
	dst io.Writer,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed: status=%d", resp.StatusCode)
	}
	return transcoder.TranscodeToMP3(ctx, resp.Body, dst, meta)
}

func downloadURLToWriter(ctx context.Context, httpClient *http.Client, streamURL string, w io.Writer) (int64, error) {
	return downloadURLToWriterWithConfig(ctx, httpClient, streamURL, w, DownloadTransportConfig{})
}

func downloadURLToWriterWithConfig(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	w io.Writer,
	cfg DownloadTransportConfig,
) (int64, error) {
	effectiveCfg := normalizeDownloadTransportConfig(cfg)
	var lastErr error
	for attempt := 0; attempt <= effectiveCfg.MaxRetries; attempt++ {
		n, err := downloadURLToWriterOnce(ctx, httpClient, streamURL, w)
		if err == nil {
			return n, nil
		}
		lastErr = err
		if !isRetryableError(err, effectiveCfg) || attempt == effectiveCfg.MaxRetries {
			return 0, err
		}
		if err := waitBackoff(ctx, effectiveCfg.backoffFor(attempt)); err != nil {
			return 0, err
		}
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, fmt.Errorf("download failed with unknown retry error")
}

func downloadURLToWriterOnce(ctx context.Context, httpClient *http.Client, streamURL string, w io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, &downloadHTTPStatusError{StatusCode: resp.StatusCode}
	}
	return io.Copy(w, resp.Body)
}

func downloadURLToPath(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	resume bool,
	cfg DownloadTransportConfig,
) (int64, error) {
	effectiveCfg := normalizeDownloadTransportConfig(cfg)
	startOffset := int64(0)
	if resume {
		if st, err := os.Stat(outputPath); err == nil {
			startOffset = st.Size()
		}
	}

	if startOffset > 0 {
		n, err := downloadURLRangeAppend(ctx, httpClient, streamURL, outputPath, startOffset, effectiveCfg)
		switch {
		case err == nil:
			return startOffset + n, nil
		case errors.Is(err, errRangeNotSatisfiable):
			return startOffset, nil
		case errors.Is(err, errRangeNotSupported):
			// fall through to full re-download from scratch
		default:
			return 0, err
		}
	}

	if effectiveCfg.EnableChunked {
		n, err := downloadURLChunked(ctx, httpClient, streamURL, outputPath, effectiveCfg)
		switch {
		case err == nil:
			return n, nil
		case errors.Is(err, errRangeNotSupported), errors.Is(err, errChunkProbeFailed):
			// fall through to full rewrite path
		default:
			return 0, err
		}
	}

	return downloadURLFullRewrite(ctx, httpClient, streamURL, outputPath, effectiveCfg)
}

var (
	errRangeNotSatisfiable = errors.New("range not satisfiable")
	errRangeNotSupported   = errors.New("range not supported")
	errChunkProbeFailed    = errors.New("chunk probe failed")
)

func downloadURLRangeAppend(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	startOffset int64,
	cfg effectiveDownloadTransportConfig,
) (int64, error) {
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		n, err := downloadRangeOnce(ctx, httpClient, streamURL, startOffset, file)
		if err == nil {
			return n, nil
		}
		if errors.Is(err, errRangeNotSatisfiable) || errors.Is(err, errRangeNotSupported) {
			return 0, err
		}
		lastErr = err
		if !isRetryableError(err, cfg) || attempt == cfg.MaxRetries {
			return 0, err
		}
		if err := waitBackoff(ctx, cfg.backoffFor(attempt)); err != nil {
			return 0, err
		}
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, fmt.Errorf("resume download failed with unknown retry error")
}

func downloadRangeOnce(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	startOffset int64,
	w io.Writer,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		return io.Copy(w, resp.Body)
	case http.StatusRequestedRangeNotSatisfiable:
		return 0, errRangeNotSatisfiable
	case http.StatusOK:
		return 0, errRangeNotSupported
	default:
		return 0, &downloadHTTPStatusError{StatusCode: resp.StatusCode}
	}
}

func downloadURLFullRewrite(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	cfg effectiveDownloadTransportConfig,
) (int64, error) {
	file, err := os.Create(outputPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return downloadURLToWriterWithConfig(ctx, httpClient, streamURL, file, DownloadTransportConfig{
		MaxRetries:       cfg.MaxRetries,
		InitialBackoff:   cfg.InitialBackoff,
		MaxBackoff:       cfg.MaxBackoff,
		RetryStatusCodes: cfg.RetryStatusCodes,
	})
}

type effectiveDownloadTransportConfig struct {
	MaxRetries       int
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	RetryStatusCodes []int
	EnableChunked    bool
	ChunkSize        int64
	MaxConcurrency   int
}

func normalizeDownloadTransportConfig(cfg DownloadTransportConfig) effectiveDownloadTransportConfig {
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
	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1 << 20 // 1 MiB
	}
	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}

	return effectiveDownloadTransportConfig{
		MaxRetries:       maxRetries,
		InitialBackoff:   initialBackoff,
		MaxBackoff:       maxBackoff,
		RetryStatusCodes: statusCodes,
		EnableChunked:    cfg.EnableChunked,
		ChunkSize:        chunkSize,
		MaxConcurrency:   maxConcurrency,
	}
}

func (c effectiveDownloadTransportConfig) backoffFor(attempt int) time.Duration {
	backoff := c.InitialBackoff
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff > c.MaxBackoff {
			return c.MaxBackoff
		}
	}
	return backoff
}

type downloadHTTPStatusError struct {
	StatusCode int
}

func (e *downloadHTTPStatusError) Error() string {
	return fmt.Sprintf("download failed: status=%d", e.StatusCode)
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

func isRetryableError(err error, cfg effectiveDownloadTransportConfig) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
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

func downloadURLChunked(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	cfg effectiveDownloadTransportConfig,
) (int64, error) {
	total, err := probeContentLengthWithRange(ctx, httpClient, streamURL)
	if err != nil {
		return 0, err
	}
	if total <= 0 {
		return 0, errChunkProbeFailed
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	if err := file.Truncate(total); err != nil {
		return 0, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	chunks := buildChunks(total, cfg.ChunkSize)
	sem := make(chan struct{}, cfg.MaxConcurrency)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for _, chunk := range chunks {
		if ctx.Err() != nil {
			break
		}
		chunk := chunk
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if err := downloadChunkWithRetry(ctx, httpClient, streamURL, file, chunk[0], chunk[1], cfg); err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
			}
		}()
	}

	wg.Wait()
	select {
	case err := <-errCh:
		return 0, err
	default:
		return total, nil
	}
}

func probeContentLengthWithRange(ctx context.Context, httpClient *http.Client, streamURL string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusPartialContent {
		return 0, errRangeNotSupported
	}
	cr := strings.TrimSpace(resp.Header.Get("Content-Range"))
	// expected form: bytes 0-0/12345
	slash := strings.LastIndex(cr, "/")
	if slash < 0 || slash == len(cr)-1 {
		return 0, errChunkProbeFailed
	}
	var total int64
	if _, err := fmt.Sscanf(cr[slash+1:], "%d", &total); err != nil || total <= 0 {
		return 0, errChunkProbeFailed
	}
	return total, nil
}

func buildChunks(total, chunkSize int64) [][2]int64 {
	if total <= 0 {
		return nil
	}
	var chunks [][2]int64
	for start := int64(0); start < total; start += chunkSize {
		end := start + chunkSize - 1
		if end >= total {
			end = total - 1
		}
		chunks = append(chunks, [2]int64{start, end})
	}
	return chunks
}

func downloadChunkWithRetry(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	file *os.File,
	start int64,
	end int64,
	cfg effectiveDownloadTransportConfig,
) error {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		err := downloadChunkOnce(ctx, httpClient, streamURL, file, start, end)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryableError(err, cfg) || attempt == cfg.MaxRetries {
			return err
		}
		if err := waitBackoff(ctx, cfg.backoffFor(attempt)); err != nil {
			return err
		}
	}
	return lastErr
}

func downloadChunkOnce(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	file *os.File,
	start int64,
	end int64,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusOK {
			return errRangeNotSupported
		}
		return &downloadHTTPStatusError{StatusCode: resp.StatusCode}
	}

	buf := make([]byte, 32*1024)
	offset := start
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.WriteAt(buf[:n], offset); writeErr != nil {
				return writeErr
			}
			offset += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if offset != end+1 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func defaultOutputPath(videoID string, itag int, mimeType string, mode SelectionMode) string {
	if mode == SelectionModeMP3 {
		return fmt.Sprintf("%s-%d.mp3", videoID, itag)
	}
	ext := ".bin"
	if mediaType, _, err := mime.ParseMediaType(mimeType); err == nil {
		if parts := strings.SplitN(mediaType, "/", 2); len(parts) == 2 && parts[1] != "" {
			ext = "." + parts[1]
		}
	}
	return fmt.Sprintf("%s-%d%s", videoID, itag, ext)
}
