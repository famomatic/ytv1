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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/famomatic/ytv1/internal/downloader"
	"github.com/famomatic/ytv1/internal/selector"
	"github.com/famomatic/ytv1/internal/types"
)

// DownloadOptions controls stream download behavior.
type DownloadOptions struct {
	Itag           int
	Mode           SelectionMode
	FormatSelector string // e.g. "bestvideo+bestaudio", overrides Mode
	OutputPath     string
	Resume         bool
	MergeOutput    bool
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

	videoID, err := normalizeVideoID(input)
	if err != nil {
		return nil, err
	}

	// filters ...

	info, err := c.GetVideo(ctx, videoID)
	if err != nil {
		return nil, err
	}
	formats := info.Formats

	meta := types.Metadata{
		Title:       info.Title,
		Artist:      info.Author,
		Description: info.Description,
		Date:        info.PublishDate,
		Duration:    int(info.DurationSec),
	}
	if meta.Date == "" {
		meta.Date = info.UploadDate
	}

	// Filter unplayable formats (e.g. requiring PO Token)
	filteredFormats, skipReasons := filterFormatsByPoTokenPolicy(formats, c.config)
	if len(filteredFormats) == 0 && len(skipReasons) > 0 {
		return nil, &NoPlayableFormatsDetailError{
			Mode:  options.Mode, // Approximate
			Skips: skipReasons,
		}
	}
	if len(filteredFormats) > 0 {
		formats = filteredFormats
	}
	if len(formats) == 0 {
		return nil, ErrNoPlayableFormats
	}

	// 1. Determine Selector
	selStr := options.FormatSelector
	if selStr == "" {
		if options.Itag > 0 {
			// Explicit Itag: No selector needed, handled in selection
		} else {
			// Map Mode to selector
			switch options.Mode {
			case SelectionModeBest, "":
				selStr = "bestvideo+bestaudio/best"
			case SelectionModeMP4AV:
				selStr = "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best"
			case SelectionModeMP4VideoOnly:
				selStr = "bestvideo[ext=mp4]"
			case SelectionModeVideoOnly:
				selStr = "bestvideo"
			case SelectionModeAudioOnly, SelectionModeMP3:
				selStr = "bestaudio"
			default:
				selStr = "best"
			}
		}
	}

	// 2. Select Formats
	var selected []types.FormatInfo
	if options.Itag > 0 {
		for _, f := range formats {
			if f.Itag == options.Itag {
				selected = []types.FormatInfo{f}
				break
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("requested itag %d not found", options.Itag)
		}
	} else {
		sel, err := selector.Parse(selStr)
		if err != nil {
			return nil, fmt.Errorf("selector parse failed: %w", err)
		}
		selected, err = selector.Select(formats, sel)
		if err != nil {
			return nil, err
		}
	}

	if len(selected) == 0 {
		return nil, errors.New("no formats found matching criteria")
	}

	// 3. Fallback for Merge if Muxer missing
	if len(selected) > 1 && (c.config.Muxer == nil || !c.config.Muxer.Available()) {
		c.logger.Warnf("Muxer unavailable, falling back to best single file")
		sel, _ := selector.Parse("best")
		selected, _ = selector.Select(formats, sel)
		if len(selected) == 0 {
			return nil, errors.New("no formats found (and muxer unavailable)")
		}
	}

	// 4. Download
	if len(selected) == 1 {
		return c.downloadSingle(ctx, videoID, selected[0], options.OutputPath, options)
	}

	return c.downloadAndMerge(ctx, videoID, selected, options, meta)
}

func (c *Client) downloadSingle(ctx context.Context, videoID string, f types.FormatInfo, outputPath string, options DownloadOptions) (*DownloadResult, error) {
	if outputPath == "" {
		outputPath = defaultOutputPath(videoID, f.Itag, f.MimeType, options.Mode)
	}
	if dir := filepath.Dir(outputPath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	// MP3 Transcode Check
	if options.Mode == SelectionModeMP3 && c.config.MP3Transcoder == nil {
		return nil, &MP3TranscoderError{Mode: options.Mode}
	}

	streamURL, err := c.ResolveStreamURL(ctx, videoID, f.Itag)
	if err != nil {
		return nil, err
	}

	// If MP3, we might need to download to temp then transcode, or stream transcode.
	// Previous logic: transcodeURLToMP3 handles download.
	if options.Mode == SelectionModeMP3 {
		out, err := os.Create(outputPath)
		if err != nil {
			return nil, err
		}
		defer out.Close()

		bytes, err := transcodeURLToMP3(ctx, c.config.HTTPClient, c.config.MP3Transcoder, streamURL, MP3TranscodeMetadata{
			VideoID: videoID, SourceItag: f.Itag, SourceMimeType: f.MimeType,
		}, out)
		if err != nil {
			return nil, err
		}

		return &DownloadResult{VideoID: videoID, Itag: f.Itag, OutputPath: outputPath, Bytes: bytes}, nil
	}

	if err := c.downloadStream(ctx, videoID, streamURL, outputPath, f); err != nil {
		return nil, err
	}

	return &DownloadResult{
		VideoID:    videoID,
		Itag:       f.Itag,
		OutputPath: outputPath,
		Bytes:      getFileSize(outputPath),
	}, nil
}

func (c *Client) downloadAndMerge(ctx context.Context, videoID string, formats []types.FormatInfo, options DownloadOptions, meta types.Metadata) (*DownloadResult, error) {
	// Identify Video and Audio
	var vidF, audF types.FormatInfo
	foundV, foundA := false, false

	for _, f := range formats {
		if f.HasVideo && !foundV {
			vidF = f
			foundV = true
		} else if f.HasAudio && !foundA {
			audF = f
			foundA = true
		}
	}

	if !foundV || !foundA {
		// Should not happen if selector logic works for +
		return c.downloadSingle(ctx, videoID, formats[0], options.OutputPath, options)
	}

	basePath := options.OutputPath
	if basePath == "" {
		basePath = fmt.Sprintf("%s-%d+%d.mp4", videoID, vidF.Itag, audF.Itag)
	}
	if filepath.Ext(basePath) == "" {
		basePath += ".mp4"
	}

	if dir := filepath.Dir(basePath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	videoPath := basePath + ".f" + strconv.Itoa(vidF.Itag) + ".video"
	audioPath := basePath + ".f" + strconv.Itoa(audF.Itag) + ".audio"

	// Video
	vURL, err := c.ResolveStreamURL(ctx, videoID, vidF.Itag)
	if err != nil {
		return nil, err
	}
	if err := c.downloadStream(ctx, videoID, vURL, videoPath, vidF); err != nil {
		return nil, err
	}
	defer os.Remove(videoPath)

	// Audio
	aURL, err := c.ResolveStreamURL(ctx, videoID, audF.Itag)
	if err != nil {
		return nil, err
	}
	if err := c.downloadStream(ctx, videoID, aURL, audioPath, audF); err != nil {
		return nil, err
	}
	defer os.Remove(audioPath)

	// Merge
	if err := c.config.Muxer.Merge(ctx, videoPath, audioPath, basePath, meta); err != nil {
		return nil, err
	}

	return &DownloadResult{
		VideoID:    videoID,
		Itag:       vidF.Itag,
		OutputPath: basePath,
		Bytes:      getFileSize(basePath),
	}, nil
}

func (c *Client) downloadStream(ctx context.Context, videoID, streamURL, outputPath string, f types.FormatInfo) error {
	if f.Protocol == "hls" || strings.HasSuffix(streamURL, ".m3u8") {
		_, err := c.downloadHLS(ctx, videoID, streamURL, outputPath, f)
		return err
	}
	if f.Protocol == "dash" || strings.HasSuffix(streamURL, ".mpd") {
		_, err := c.downloadDASH(ctx, videoID, streamURL, outputPath, f)
		return err
	}
	_, err := downloadURLToPath(ctx, c.config.HTTPClient, streamURL, outputPath, false, c.config.DownloadTransport)
	return err
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

func (c *Client) downloadHLS(ctx context.Context, videoID, streamURL, outputPath string, format FormatInfo) (*DownloadResult, error) {
	dl := downloader.NewHLSDownloader(c.config.HTTPClient, streamURL)

	f, err := os.Create(outputPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if err := dl.Download(ctx, f); err != nil {
		return nil, err
	}

	f.Sync()

	info, err := os.Stat(outputPath)
	size := int64(0)
	if err == nil {
		size = info.Size()
	}

	return &DownloadResult{
		VideoID:    videoID,
		Itag:       format.Itag,
		OutputPath: outputPath,
		Bytes:      size,
	}, nil
}

func (c *Client) downloadDASH(ctx context.Context, videoID, streamURL, outputPath string, format FormatInfo) (*DownloadResult, error) {
	repID := fmt.Sprintf("%d", format.Itag)
	dl := downloader.NewDASHDownloader(c.config.HTTPClient, streamURL, repID)

	f, err := os.Create(outputPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if err := dl.Download(ctx, f); err != nil {
		return nil, err
	}

	f.Sync()

	info, err := os.Stat(outputPath)
	size := int64(0)
	if err == nil {
		size = info.Size()
	}

	return &DownloadResult{
		VideoID:    videoID,
		Itag:       format.Itag,
		OutputPath: outputPath,
		Bytes:      size,
	}, nil
}

func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
