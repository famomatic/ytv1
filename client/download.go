package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/famomatic/ytv1/internal/downloader"
	"github.com/famomatic/ytv1/internal/source"
	"github.com/famomatic/ytv1/internal/types"
)

var (
	createFile = os.Create
	openFile   = os.OpenFile
	removeFile = os.Remove
	renameFile = os.Rename
)

// DownloadOptions controls stream download behavior.
type DownloadOptions struct {
	Itag                  int
	Mode                  SelectionMode
	FormatSelector        string // e.g. "bestvideo+bestaudio", overrides Mode
	OutputPath            string
	Resume                bool
	MergeOutput           bool
	KeepIntermediateFiles bool
	UsePartFiles          bool
	AudioQuality          string
	NoEmbedMetadata       bool
	MergeOutputFormat     string
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
// If options.OutputPath is empty, the yt-dlp-style default
// "<title> [<videoID>].<ext>" is used.
func (c *Client) Download(ctx context.Context, input string, options DownloadOptions) (*DownloadResult, error) {
	ctx, cancel := withDefaultTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	// Non-YouTube sources (e.g. SOOP) run a dedicated extract+download path.
	if src := c.matchSource(input); src != nil {
		return c.downloadViaSource(ctx, src, input, options)
	}

	videoID, err := normalizeVideoID(input)
	if err != nil {
		return nil, err
	}

	// filters ...

	var info *VideoInfo
	if session, ok := c.getSession(videoID); ok && session.Info != nil {
		info = cloneVideoInfo(session.Info)
	}
	if info == nil {
		info, err = c.GetVideo(ctx, videoID)
		if err != nil {
			return nil, err
		}
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
		for _, skip := range skipReasons {
			c.warnf("format skipped by po token policy: itag=%d protocol=%s reason=%s", skip.Itag, skip.Protocol, skip.Reason)
		}
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

	selected, err := SelectFormatsForDownloadOptions(formats, options)
	if err != nil {
		return nil, err
	}

	// Do not silently downgrade separate best video+audio selections to a
	// lower progressive stream just because local merge support is unavailable.
	if len(selected) > 1 && (c.config.Muxer == nil || !c.config.Muxer.Available()) {
		return nil, fmt.Errorf("%w: selected formats require merge; configure ffmpeg or choose a single progressive format", ErrMuxerUnavailable)
	}

	if len(selected) == 1 {
		res, err := c.downloadSingle(ctx, videoID, info.Title, info.Author, selected[0], options.OutputPath, options)
		if shouldRetryDefaultWithFallbackSingle(err, options) {
			c.warnf("selected media download failed; retrying with fallback single-file format")
			return c.downloadFallbackSingleWithRefresh(ctx, videoID, info.Title, info.Author, formats, options.OutputPath, options)
		}
		return res, err
	}

	res, err := c.downloadAndMerge(ctx, videoID, selected, options, meta)
	if shouldRetryDefaultWithFallbackSingle(err, options) {
		c.warnf("selected media download failed during merge selection; retrying with fallback single-file format")
		return c.downloadFallbackSingleWithRefresh(ctx, videoID, info.Title, info.Author, formats, options.OutputPath, options)
	}
	return res, err
}

// downloadViaSource extracts through a non-YouTube source and downloads the
// selected format(s) using the shared download pipeline. The source's media
// headers (e.g. SOOP Referer/Origin) are cached on the session so the HLS/HTTP
// downloaders attach them to manifest and segment requests.
func (c *Client) downloadViaSource(ctx context.Context, src source.Source, input string, options DownloadOptions) (*DownloadResult, error) {
	// Reuse the session a prior GetVideo cached for this exact input instead of
	// re-extracting. A second Extract would double the source's API calls and,
	// for SOOP live, re-run token resolution and a second agent media probe.
	if s, ok := c.getSession(sourceInputKey(src.Name(), input)); ok && s.Info != nil {
		return c.downloadSourceSession(ctx, src.Name(), cloneVideoInfo(s.Info), s.MediaHeaders, options)
	}

	media, err := src.Extract(ctx, input)
	if err != nil {
		return nil, err
	}
	if media == nil || len(media.Formats) == 0 {
		return nil, ErrNoPlayableFormats
	}
	info := mediaToVideoInfo(media)
	c.cacheSourceSession(src.Name(), input, info, media.MediaHeaders)
	return c.downloadSourceSession(ctx, src.Name(), info, media.MediaHeaders, options)
}

// downloadSourceSession selects and downloads from an already-extracted source
// result. It is shared by the fresh-extract and cached-session paths of
// downloadViaSource. The media-id session must exist so downloadSingle /
// downloadAndMerge can resolve URLs and attach source media headers.
func (c *Client) downloadSourceSession(ctx context.Context, name string, info *VideoInfo, headers http.Header, options DownloadOptions) (*DownloadResult, error) {
	id := sourceSessionKey(name, info.ID)
	if _, ok := c.getSession(id); !ok {
		c.putSession(id, videoSession{
			Info:         cloneVideoInfo(info),
			MediaHeaders: headers,
			SourceName:   name,
		})
	}

	meta := types.Metadata{
		Title:       info.Title,
		Artist:      info.Author,
		Description: info.Description,
		Date:        info.UploadDate,
		Duration:    int(info.DurationSec),
	}

	selected, err := SelectFormatsForDownloadOptions(info.Formats, options)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, ErrNoPlayableFormats
	}
	if len(selected) > 1 && (c.config.Muxer == nil || !c.config.Muxer.Available()) {
		return nil, fmt.Errorf("%w: selected formats require merge; configure ffmpeg or choose a single format", ErrMuxerUnavailable)
	}

	if len(selected) == 1 {
		return c.downloadSingle(ctx, id, info.Title, info.Author, selected[0], options.OutputPath, options)
	}
	return c.downloadAndMerge(ctx, id, selected, options, meta)
}

// applySourceMediaHeaders overrides base media request headers with any
// source-specific headers cached for videoID (e.g. SOOP Referer/Origin/UA).
// Source headers replace matching keys so exactly one value is sent. For
// YouTube (no cached source headers) base is returned unchanged.
func (c *Client) applySourceMediaHeaders(videoID string, base http.Header) http.Header {
	extra := c.sessionMediaHeaders(videoID)
	if len(extra) == 0 {
		return base
	}
	if base == nil {
		base = make(http.Header)
	}
	for k, vals := range extra {
		base.Del(k)
		for _, v := range vals {
			base.Add(k, v)
		}
	}
	return base
}

func shouldRetryDefaultWithFallbackSingle(err error, options DownloadOptions) bool {
	if err == nil || options.Itag != 0 || strings.TrimSpace(options.FormatSelector) != "" {
		return false
	}
	if errors.Is(err, ErrChallengeNotSolved) {
		return true
	}
	return downloadFailureHTTPStatus(err) == http.StatusForbidden
}

func downloadFailureHTTPStatus(err error) int {
	var statusErr *downloadHTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode
	}
	var detail *DownloadFailureDetailError
	if errors.As(err, &detail) {
		for _, attempt := range detail.Attempts {
			if attempt.HTTPStatus != 0 {
				return attempt.HTTPStatus
			}
		}
	}
	return 0
}

func (c *Client) downloadFallbackSingleWithRefresh(
	ctx context.Context,
	videoID string,
	title string,
	uploader string,
	formats []types.FormatInfo,
	outputPath string,
	options DownloadOptions,
) (*DownloadResult, error) {
	res, err := c.downloadFallbackSingle(ctx, videoID, title, uploader, formats, outputPath, options)
	if downloadFailureHTTPStatus(err) != http.StatusForbidden {
		return res, err
	}

	c.warnf("fallback single-file URL returned 403; refreshing extraction and retrying once")
	c.deleteSession(videoID)
	info, refreshErr := c.GetVideo(ctx, videoID)
	if refreshErr != nil {
		return nil, err
	}
	return c.downloadFallbackSingle(ctx, videoID, info.Title, info.Author, info.Formats, outputPath, options)
}

func (c *Client) downloadFallbackSingle(
	ctx context.Context,
	videoID string,
	title string,
	uploader string,
	formats []types.FormatInfo,
	outputPath string,
	options DownloadOptions,
) (*DownloadResult, error) {
	candidates := make([]types.FormatInfo, 0, len(formats))
	for _, f := range formats {
		if f.HasVideo && f.HasAudio {
			candidates = append(candidates, f)
		}
	}
	if len(candidates) == 0 {
		return nil, ErrChallengeNotSolved
	}

	preferred := make([]types.FormatInfo, 0, len(candidates))
	for _, f := range candidates {
		if !f.Ciphered {
			preferred = append(preferred, f)
		}
	}
	if len(preferred) == 0 {
		preferred = candidates
	}

	for _, f := range preferred {
		res, err := c.downloadSingle(ctx, videoID, title, uploader, f, outputPath, options)
		if err == nil {
			return res, nil
		}
		if !errors.Is(err, ErrChallengeNotSolved) {
			return nil, err
		}
	}
	return nil, ErrChallengeNotSolved
}

func (c *Client) downloadSingle(ctx context.Context, videoID string, title string, uploader string, f types.FormatInfo, outputPath string, options DownloadOptions) (*DownloadResult, error) {
	if outputPath == "" {
		outputPath = defaultOutputPath(videoID, title, f.Itag, f.MimeType, options.Mode)
	} else {
		outputPath = renderOutputPathTemplate(outputPath, outputTemplateData{
			VideoID:  videoID,
			Title:    title,
			Uploader: uploader,
			Ext:      detectOutputExt(f.MimeType, options.Mode),
			Itag:     strconv.Itoa(f.Itag),
		})
		if strings.TrimSpace(outputPath) == "" {
			outputPath = defaultOutputPath(videoID, title, f.Itag, f.MimeType, options.Mode)
		}
	}
	if dir := filepath.Dir(outputPath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	// MP3 Transcode Check
	if options.Mode == SelectionModeMP3 && c.config.MP3Transcoder == nil {
		return nil, &MP3TranscoderError{Mode: options.Mode}
	}

	streamURL, err := c.resolveSelectedFormatURL(ctx, videoID, f)
	if err != nil {
		return nil, err
	}
	c.emitDownloadEvent("download", "destination", videoID, outputPath, fmt.Sprintf("itag=%d", f.Itag))

	// If MP3, we might need to download to temp then transcode, or stream transcode.
	// Previous logic: transcodeURLToMP3 handles download.
	if options.Mode == SelectionModeMP3 {
		c.emitDownloadEvent("download", "start", videoID, outputPath, "transcode=mp3")
		var out *os.File
		if err := retryFileAccess(ctx, c.config.DownloadTransport, func() error {
			var err error
			out, err = createFile(outputPath)
			return err
		}); err != nil {
			c.emitDownloadEvent("download", "failure", videoID, outputPath, err.Error())
			return nil, err
		}
		defer out.Close()

		bytes, err := transcodeURLToMP3(ctx, c.config.HTTPClient, c.config.MP3Transcoder, streamURL, MP3TranscodeMetadata{
			VideoID:        videoID,
			SourceItag:     f.Itag,
			SourceMimeType: f.MimeType,
			SourceClient:   f.SourceClient,
			AudioQuality:   options.AudioQuality,
		}, out, c.config.RequestHeaders, c.config.DownloadTransport.RateLimitBytesPerSecond)
		if err != nil {
			c.emitDownloadEvent("download", "failure", videoID, outputPath, err.Error())
			return nil, err
		}
		c.emitDownloadEvent("download", "complete", videoID, outputPath, fmt.Sprintf("bytes=%d", bytes))

		return &DownloadResult{VideoID: videoID, Itag: f.Itag, OutputPath: outputPath, Bytes: bytes}, nil
	}

	c.emitDownloadEvent("download", "start", videoID, outputPath, fmt.Sprintf("itag=%d", f.Itag))
	if err := c.downloadStream(ctx, videoID, streamURL, outputPath, f, options.Resume, options.UsePartFiles); err != nil {
		attempt := downloadAttemptFromFormatAndURL(f, streamURL, err)
		c.emitDownloadEvent("download", "failure", videoID, outputPath, formatDownloadFailureDetail(attempt))
		return nil, wrapDownloadFailure(err, attempt)
	}
	c.emitDownloadEvent("download", "complete", videoID, outputPath, fmt.Sprintf("bytes=%d", getFileSize(outputPath)))

	return &DownloadResult{
		VideoID:    videoID,
		Itag:       f.Itag,
		OutputPath: outputPath,
		Bytes:      getFileSize(outputPath),
	}, nil
}

func (c *Client) downloadAndMerge(ctx context.Context, videoID string, formats []types.FormatInfo, options DownloadOptions, meta types.Metadata) (*DownloadResult, error) {
	if options.NoEmbedMetadata {
		meta = types.Metadata{}
	}

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
		return c.downloadSingle(ctx, videoID, meta.Title, meta.Artist, formats[0], options.OutputPath, options)
	}

	basePath := options.OutputPath
	mergeExt := defaultMergeExtForFormats(options.MergeOutputFormat, vidF, audF)
	if basePath == "" {
		basePath = defaultOutputPathForExt(videoID, meta.Title, fmt.Sprintf("%d+%d", vidF.Itag, audF.Itag), mergeExt)
	} else {
		basePath = renderOutputPathTemplate(basePath, outputTemplateData{
			VideoID:  videoID,
			Title:    meta.Title,
			Uploader: meta.Artist,
			Ext:      mergeExt,
			Itag:     fmt.Sprintf("%d+%d", vidF.Itag, audF.Itag),
		})
		if strings.TrimSpace(basePath) == "" {
			basePath = defaultOutputPathForExt(videoID, meta.Title, fmt.Sprintf("%d+%d", vidF.Itag, audF.Itag), mergeExt)
		}
	}
	if filepath.Ext(basePath) == "" {
		basePath += "." + mergeExt
	}

	if dir := filepath.Dir(basePath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	videoPath := basePath + ".f" + strconv.Itoa(vidF.Itag) + ".video"
	audioPath := basePath + ".f" + strconv.Itoa(audF.Itag) + ".audio"
	keepIntermediates := options.KeepIntermediateFiles || c.config.KeepIntermediateFiles

	// Video
	vURL, err := c.resolveSelectedFormatURL(ctx, videoID, vidF)
	if err != nil {
		return nil, err
	}
	c.emitDownloadEvent("download", "destination", videoID, videoPath, fmt.Sprintf("itag=%d", vidF.Itag))
	c.emitDownloadEvent("download", "start", videoID, videoPath, fmt.Sprintf("itag=%d", vidF.Itag))
	if err := c.downloadStream(ctx, videoID, vURL, videoPath, vidF, options.Resume, options.UsePartFiles); err != nil {
		attempt := downloadAttemptFromFormatAndURL(vidF, vURL, err)
		c.emitDownloadEvent("download", "failure", videoID, videoPath, formatDownloadFailureDetail(attempt))
		c.cleanupIntermediateFile(videoID, videoPath, keepIntermediates)
		c.cleanupIntermediateFile(videoID, videoPath+".part", keepIntermediates)
		return nil, wrapDownloadFailure(err, attempt)
	}
	c.emitDownloadEvent("download", "complete", videoID, videoPath, fmt.Sprintf("bytes=%d", getFileSize(videoPath)))
	defer c.cleanupIntermediateFile(videoID, videoPath, keepIntermediates)

	// Audio
	aURL, err := c.resolveSelectedFormatURL(ctx, videoID, audF)
	if err != nil {
		return nil, err
	}
	c.emitDownloadEvent("download", "destination", videoID, audioPath, fmt.Sprintf("itag=%d", audF.Itag))
	c.emitDownloadEvent("download", "start", videoID, audioPath, fmt.Sprintf("itag=%d", audF.Itag))
	if err := c.downloadStream(ctx, videoID, aURL, audioPath, audF, options.Resume, options.UsePartFiles); err != nil {
		attempt := downloadAttemptFromFormatAndURL(audF, aURL, err)
		c.emitDownloadEvent("download", "failure", videoID, audioPath, formatDownloadFailureDetail(attempt))
		c.cleanupIntermediateFile(videoID, audioPath, keepIntermediates)
		c.cleanupIntermediateFile(videoID, audioPath+".part", keepIntermediates)
		return nil, wrapDownloadFailure(err, attempt)
	}
	c.emitDownloadEvent("download", "complete", videoID, audioPath, fmt.Sprintf("bytes=%d", getFileSize(audioPath)))
	defer c.cleanupIntermediateFile(videoID, audioPath, keepIntermediates)

	// Final guard before merge: reject empty intermediate files. A zero-byte
	// video or audio track would produce a broken merged file that plays
	// with corruption or no media.
	if getFileSize(videoPath) == 0 {
		c.cleanupIntermediateFile(videoID, videoPath, keepIntermediates)
		return nil, &TruncatedDownloadError{Expected: vidF.ContentLength, Actual: 0, Itag: vidF.Itag}
	}
	if getFileSize(audioPath) == 0 {
		c.cleanupIntermediateFile(videoID, audioPath, keepIntermediates)
		return nil, &TruncatedDownloadError{Expected: audF.ContentLength, Actual: 0, Itag: audF.Itag}
	}

	// Merge
	c.emitDownloadEvent("merge", "start", videoID, basePath, fmt.Sprintf("video_itag=%d,audio_itag=%d", vidF.Itag, audF.Itag))
	if err := c.config.Muxer.Merge(ctx, videoPath, audioPath, basePath, meta); err != nil {
		c.emitDownloadEvent("merge", "failure", videoID, basePath, err.Error())
		return nil, err
	}
	c.emitDownloadEvent("merge", "complete", videoID, basePath, fmt.Sprintf("bytes=%d", getFileSize(basePath)))

	return &DownloadResult{
		VideoID:    videoID,
		Itag:       vidF.Itag,
		OutputPath: basePath,
		Bytes:      getFileSize(basePath),
	}, nil
}

func (c *Client) downloadStream(ctx context.Context, videoID, streamURL, outputPath string, f types.FormatInfo, resume bool, usePartFiles bool) error {
	// `-o -` streams the media to stdout (e.g. `ytv1 <url> -o - | mpv -` for a
	// live player). Dispatch by protocol so the bytes on stdout are always media:
	// an HLS/DASH format must run through its segment downloader (a plain GET of
	// a .m3u8/.mpd would pipe the manifest text, not media); only a truly direct
	// stream (e.g. the SOOP agent's live MPEG-TS) is copied straight through. No
	// part file / resume / range so a live feed flows uninterrupted.
	if outputPath == "-" {
		switch {
		case f.Protocol == "hls" || strings.HasSuffix(streamURL, ".m3u8"):
			playlistURLs := f.Parts
			if len(playlistURLs) == 0 {
				playlistURLs = []string{streamURL}
			}
			return c.hlsStreamTo(ctx, videoID, playlistURLs, os.Stdout, "-", f)
		case f.Protocol == "dash" || strings.HasSuffix(streamURL, ".mpd"):
			return c.dashStreamTo(ctx, videoID, streamURL, os.Stdout, "-", f)
		default:
			return c.streamToWriter(ctx, videoID, streamURL, f, os.Stdout)
		}
	}
	if f.Protocol == "hls" || strings.HasSuffix(streamURL, ".m3u8") {
		// A multi-part format (e.g. a SOOP VOD split across files) carries an
		// ordered list of per-part playlists; fall back to the single resolved
		// URL otherwise.
		playlistURLs := f.Parts
		if len(playlistURLs) == 0 {
			playlistURLs = []string{streamURL}
		}
		_, err := c.downloadHLS(ctx, videoID, playlistURLs, outputPath, f, usePartFiles)
		return err
	}
	if f.Protocol == "dash" || strings.HasSuffix(streamURL, ".mpd") {
		_, err := c.downloadDASH(ctx, videoID, streamURL, outputPath, f, usePartFiles)
		return err
	}
	_, err := downloadURLToPathWithHeadersAndPartProgress(
		ctx,
		c.config.HTTPClient,
		streamURL,
		outputPath,
		resume,
		usePartFiles,
		c.config.DownloadTransport,
		videoID,
		c.applySourceMediaHeaders(videoID, buildMediaRequestHeadersForSourceClient(c.config.RequestHeaders, videoID, f.SourceClient)),
		newDownloadProgressReporter(c.config.OnDownloadProgress, videoID, outputPath, f.Itag, inferProgressPart(outputPath)),
	)
	if err != nil {
		return err
	}
	// Post-download integrity check: verify the file size matches the
	// expected ContentLength reported by YouTube. A mismatch means the
	// stream was silently truncated (server closed early, partial write).
	if f.ContentLength > 0 {
		actual := getFileSize(outputPath)
		if actual < f.ContentLength {
			return &TruncatedDownloadError{
				Expected: f.ContentLength,
				Actual:   actual,
				Itag:     f.Itag,
			}
		}
	}
	return nil
}

// streamToWriter does a single plain GET and copies the response body to w. Used
// for `-o -` (stdout piping) of a non-segmented stream (e.g. the SOOP agent's
// live MPEG-TS); no ranges/resume so a live stream flows through. Source media
// headers (e.g. SOOP Referer/Origin) are applied so a non-YouTube CDN does not
// reject the request as it would with the default YouTube headers.
func (c *Client) streamToWriter(ctx context.Context, videoID, streamURL string, f types.FormatInfo, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	applyRequestHeaders(req, c.applySourceMediaHeaders(videoID, buildMediaRequestHeadersForSourceClient(c.config.RequestHeaders, videoID, f.SourceClient)))
	resp, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status=%d", resp.StatusCode)
	}
	return copyWithWriteStallGuard(ctx, w, resp.Body, stdoutStallTimeout())
}

// stdoutStallTimeout is how long a single write to the `-o -` sink may block
// before the stream is treated as consumer-dead. A player closing behind a shell
// pipe (notably PowerShell) can leave ytv1's stdout pipe open but undrained, so
// the blocking write never errors and the process would pull a live stream from
// the source forever. YTV1_STDOUT_STALL_SECS overrides it; 0 disables the guard
// (e.g. when long player pauses are wanted).
func stdoutStallTimeout() time.Duration {
	if v := os.Getenv("YTV1_STDOUT_STALL_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 30 * time.Second
}

// copyWithWriteStallGuard copies src→dst like io.Copy, but fails if a single
// dst.Write blocks longer than stall (0 disables the guard). It flags only a
// stuck *write*, never a slow read, so a slow source is unaffected — the guard
// fires only when the consumer has stopped reading entirely (e.g. a closed
// player behind an undraining pipe). On stall it returns an error; the blocked
// write goroutine is abandoned and the process exits, releasing the source.
func copyWithWriteStallGuard(ctx context.Context, dst io.Writer, src io.Reader, stall time.Duration) error {
	if stall <= 0 {
		_, err := io.Copy(dst, src)
		return err
	}
	var writeStartedNanos atomic.Int64 // start of an in-progress write; 0 when idle
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 128*1024)
		for {
			n, rerr := src.Read(buf)
			if n > 0 {
				writeStartedNanos.Store(time.Now().UnixNano())
				_, werr := dst.Write(buf[:n])
				writeStartedNanos.Store(0)
				if werr != nil {
					done <- werr
					return
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					rerr = nil
				}
				done <- rerr
				return
			}
		}
	}()

	ticker := time.NewTicker(stall / 3)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if ns := writeStartedNanos.Load(); ns != 0 && time.Since(time.Unix(0, ns)) > stall {
				return fmt.Errorf("output stalled: consumer read nothing for %s (player closed?)", stall)
			}
		}
	}
}

func transcodeURLToMP3(
	ctx context.Context,
	httpClient *http.Client,
	transcoder MP3Transcoder,
	streamURL string,
	meta MP3TranscodeMetadata,
	dst io.Writer,
	requestHeaders http.Header,
	rateLimitBytesPerSecond int64,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	applyMediaRequestHeadersForSourceClient(req, requestHeaders, meta.VideoID, meta.SourceClient)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed: status=%d", resp.StatusCode)
	}
	return transcoder.TranscodeToMP3(ctx, rateLimitedReader(ctx, resp.Body, rateLimitBytesPerSecond), dst, meta)
}

func downloadURLToWriter(ctx context.Context, httpClient *http.Client, streamURL string, w io.Writer) (int64, error) {
	return downloadURLToWriterWithConfigAndHeaders(ctx, httpClient, streamURL, w, DownloadTransportConfig{}, "", nil)
}

func downloadURLToWriterWithConfig(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	w io.Writer,
	cfg DownloadTransportConfig,
) (int64, error) {
	return downloadURLToWriterWithConfigAndHeaders(ctx, httpClient, streamURL, w, cfg, "", nil)
}

func downloadURLToWriterWithConfigAndHeaders(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	w io.Writer,
	cfg DownloadTransportConfig,
	videoID string,
	requestHeaders http.Header,
) (int64, error) {
	effectiveCfg := normalizeDownloadTransportConfig(cfg)
	var lastErr error
	for attempt := 0; attempt <= effectiveCfg.MaxRetries; attempt++ {
		var attemptBuf bytes.Buffer
		n, err := downloadURLToWriterOnce(ctx, httpClient, streamURL, &attemptBuf, videoID, requestHeaders, effectiveCfg)
		if err == nil {
			if _, writeErr := w.Write(attemptBuf.Bytes()); writeErr != nil {
				return 0, writeErr
			}
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

func downloadURLToWriterOnce(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	w io.Writer,
	videoID string,
	requestHeaders http.Header,
	cfg effectiveDownloadTransportConfig,
) (int64, error) {
	return downloadURLToWriterOnceProgress(ctx, httpClient, streamURL, w, videoID, requestHeaders, cfg, nil)
}

func downloadURLToWriterOnceProgress(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	w io.Writer,
	videoID string,
	requestHeaders http.Header,
	cfg effectiveDownloadTransportConfig,
	progress *downloadProgressReporter,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	applyMediaRequestHeaders(req, requestHeaders, videoID)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, &downloadHTTPStatusError{StatusCode: resp.StatusCode}
	}
	if progress != nil {
		progress.setTotal(resp.ContentLength)
	}
	// Propagate the server-advertised Content-Length into the copy config so
	// that a premature connection close (short read + clean EOF) is detected
	// instead of silently producing a truncated file.
	copyCfg := cfg
	if resp.ContentLength > 0 {
		copyCfg.expectedContentLength = resp.ContentLength
	}
	return copyWithDownloadConfig(ctx, w, resp.Body, copyCfg, progress)
}

func downloadURLToPath(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	resume bool,
	cfg DownloadTransportConfig,
) (int64, error) {
	return downloadURLToPathWithHeaders(ctx, httpClient, streamURL, outputPath, resume, cfg, "", nil)
}

func downloadURLToPathWithHeaders(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	resume bool,
	cfg DownloadTransportConfig,
	videoID string,
	requestHeaders http.Header,
) (int64, error) {
	return downloadURLToPathWithHeadersAndPart(ctx, httpClient, streamURL, outputPath, resume, false, cfg, videoID, requestHeaders)
}

func downloadURLToPathWithHeadersAndPart(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	resume bool,
	usePartFile bool,
	cfg DownloadTransportConfig,
	videoID string,
	requestHeaders http.Header,
) (int64, error) {
	return downloadURLToPathWithHeadersAndPartProgress(ctx, httpClient, streamURL, outputPath, resume, usePartFile, cfg, videoID, requestHeaders, nil)
}

func downloadURLToPathWithHeadersAndPartProgress(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	resume bool,
	usePartFile bool,
	cfg DownloadTransportConfig,
	videoID string,
	requestHeaders http.Header,
	progress *downloadProgressReporter,
) (int64, error) {
	if usePartFile {
		return downloadURLToPathPart(ctx, httpClient, streamURL, outputPath, resume, cfg, videoID, requestHeaders, progress)
	}
	effectiveCfg := normalizeDownloadTransportConfig(cfg)
	startOffset := int64(0)
	if resume {
		if st, err := os.Stat(outputPath); err == nil {
			startOffset = st.Size()
		}
	}

	if startOffset > 0 {
		if progress != nil {
			progress.add(startOffset, false)
		}
		n, err := downloadURLRangeAppend(ctx, httpClient, streamURL, outputPath, startOffset, effectiveCfg, videoID, requestHeaders, progress)
		switch {
		case err == nil:
			if progress != nil {
				progress.finish()
			}
			return startOffset + n, nil
		case errors.Is(err, errRangeNotSatisfiable):
			if progress != nil {
				progress.finish()
			}
			return startOffset, nil
		case errors.Is(err, errRangeNotSupported):
			// fall through to full re-download from scratch
		default:
			return 0, err
		}
	}

	if effectiveCfg.EnableChunked {
		n, err := downloadURLChunked(ctx, httpClient, streamURL, outputPath, effectiveCfg, videoID, requestHeaders, progress)
		switch {
		case err == nil:
			if progress != nil {
				progress.finish()
			}
			return n, nil
		case errors.Is(err, errRangeNotSupported), errors.Is(err, errChunkProbeFailed), isChunkedMediaAccessDenied(err):
			// fall through to full rewrite path
		default:
			return 0, err
		}
	}

	n, err := downloadURLFullRewrite(ctx, httpClient, streamURL, outputPath, effectiveCfg, videoID, requestHeaders, progress)
	if err == nil && progress != nil {
		progress.finish()
	}
	return n, err
}

func downloadURLToPathPart(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	resume bool,
	cfg DownloadTransportConfig,
	videoID string,
	requestHeaders http.Header,
	progress *downloadProgressReporter,
) (int64, error) {
	partPath := outputPath + ".part"
	partProgress := progress
	if partProgress != nil {
		partProgress = partProgress.withPath(partPath)
	}
	n, err := downloadURLToPathWithHeadersAndPartProgress(ctx, httpClient, streamURL, partPath, resume, false, cfg, videoID, requestHeaders, partProgress)
	if err != nil {
		return 0, err
	}
	if err := retryFileAccess(ctx, cfg, func() error {
		return renameFile(partPath, outputPath)
	}); err != nil {
		return 0, err
	}
	return n, nil
}

func retryFileAccess(ctx context.Context, cfg DownloadTransportConfig, op func() error) error {
	return retryFileAccessWithBackoff(ctx, normalizeFileAccessRetries(cfg.FileAccessRetries), cfg.FileAccessBackoff, op)
}

func retryFileAccessWithBackoff(ctx context.Context, retries int, backoff time.Duration, op func() error) error {
	if retries < 0 {
		retries = 0
	}
	if backoff <= 0 {
		backoff = 10 * time.Millisecond
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if err := op(); err != nil {
			lastErr = err
		} else {
			return nil
		}
		if attempt == retries {
			break
		}
		if err := waitBackoff(ctx, backoff); err != nil {
			return err
		}
	}
	return lastErr
}

func normalizeFileAccessRetries(retries int) int {
	if retries == 0 {
		return 3
	}
	if retries < 0 {
		return 0
	}
	return retries
}

func copyWithRateLimit(ctx context.Context, dst io.Writer, src io.Reader, bytesPerSecond int64) (int64, error) {
	return io.Copy(dst, rateLimitedReader(ctx, src, bytesPerSecond))
}

func copyWithDownloadConfig(ctx context.Context, dst io.Writer, src io.Reader, cfg effectiveDownloadTransportConfig, progress *downloadProgressReporter) (int64, error) {
	reader := rateLimitedReader(ctx, src, cfg.RateLimitBytesPerSecond)
	if cfg.ThrottledRateBytesPerSecond > 0 {
		reader = throttledRateReader(ctx, reader, cfg.ThrottledRateBytesPerSecond, cfg.ThrottledRateMinDuration)
	}
	if progress != nil {
		reader = &progressReader{src: reader, progress: progress}
	}
	n, err := io.Copy(dst, reader)
	if err != nil {
		// Map premature EOF from a short Content-Length read to a typed
		// truncation error so callers can distinguish it from hard I/O
		// failures and retry appropriately.
		if errors.Is(err, io.ErrUnexpectedEOF) && cfg.expectedContentLength > 0 && n < cfg.expectedContentLength {
			return n, &TruncatedDownloadError{
				Expected: cfg.expectedContentLength,
				Actual:   n,
			}
		}
		return n, err
	}
	// Detect premature EOF: if the server advertised a Content-Length but
	// closed the connection before delivering all bytes, io.Copy returns
	// (n, nil) because the HTTP body reader treats a short read followed by
	// a clean close as a normal EOF. Without this check the caller would
	// silently accept a truncated file.
	if cfg.expectedContentLength > 0 && n < cfg.expectedContentLength {
		return n, &TruncatedDownloadError{
			Expected: cfg.expectedContentLength,
			Actual:   n,
		}
	}
	return n, nil
}

func rateLimitedReader(ctx context.Context, src io.Reader, bytesPerSecond int64) io.Reader {
	if bytesPerSecond <= 0 {
		return src
	}
	return &rateLimitReader{
		ctx:            ctx,
		src:            src,
		bytesPerSecond: bytesPerSecond,
		start:          time.Now(),
	}
}

type rateLimitReader struct {
	ctx            context.Context
	src            io.Reader
	bytesPerSecond int64
	start          time.Time
	read           int64
}

type progressReader struct {
	src      io.Reader
	progress *downloadProgressReporter
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.progress.add(int64(n), false)
	}
	return n, err
}

type downloadProgressReporter struct {
	mu         sync.Mutex
	fn         func(DownloadProgressEvent)
	videoID    string
	path       string
	itag       int
	part       string
	total      int64
	downloaded int64
	// totalDuration/downloadedDuration track playback seconds for segmented
	// (HLS/DASH) downloads whose byte total is unknown, enabling a
	// duration-based percent and ETA.
	totalDuration      float64
	downloadedDuration float64
	started            time.Time
	lastEmit           time.Time
	// final is set by finish() so the terminal event carries Final=true.
	final bool
}

func newDownloadProgressReporter(fn func(DownloadProgressEvent), videoID string, path string, itag int, part string) *downloadProgressReporter {
	if fn == nil {
		return nil
	}
	now := time.Now()
	return &downloadProgressReporter{
		fn:       fn,
		videoID:  videoID,
		path:     path,
		itag:     itag,
		part:     part,
		started:  now,
		lastEmit: now.Add(-time.Second),
	}
}

func (p *downloadProgressReporter) withPath(path string) *downloadProgressReporter {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return &downloadProgressReporter{
		fn:       p.fn,
		videoID:  p.videoID,
		path:     path,
		itag:     p.itag,
		part:     p.part,
		started:  time.Now(),
		lastEmit: time.Now().Add(-time.Second),
	}
}

func (p *downloadProgressReporter) reset() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.downloaded = 0
	p.total = 0
	p.final = false
	p.started = time.Now()
	p.lastEmit = p.started.Add(-time.Second)
	p.mu.Unlock()
}

func (p *downloadProgressReporter) setTotal(total int64) {
	if p == nil || total <= 0 {
		return
	}
	p.mu.Lock()
	p.total = total
	p.mu.Unlock()
}

// setTotalDuration records the media's total playback duration (seconds) so a
// segmented download with no known byte total can report a duration-based
// percent and ETA.
func (p *downloadProgressReporter) setTotalDuration(seconds float64) {
	if p == nil || seconds <= 0 {
		return
	}
	p.mu.Lock()
	p.totalDuration = seconds
	p.mu.Unlock()
}

// addDuration advances the downloaded playback duration by a segment's length
// and emits a throttled progress event. The byte counters keep feeding the
// live throughput number; duration drives percent/ETA.
func (p *downloadProgressReporter) addDuration(seconds float64) {
	if p == nil || seconds <= 0 {
		return
	}
	p.mu.Lock()
	p.downloadedDuration += seconds
	now := time.Now()
	if now.Sub(p.lastEmit) < 200*time.Millisecond {
		p.mu.Unlock()
		return
	}
	p.lastEmit = now
	evt := p.eventLocked(now)
	p.mu.Unlock()
	p.fn(evt)
}

func (p *downloadProgressReporter) add(n int64, force bool) {
	if p == nil || n < 0 {
		return
	}
	p.mu.Lock()
	if n > 0 {
		p.downloaded += n
	}
	now := time.Now()
	if !force && now.Sub(p.lastEmit) < 200*time.Millisecond {
		p.mu.Unlock()
		return
	}
	p.lastEmit = now
	evt := p.eventLocked(now)
	p.mu.Unlock()
	p.fn(evt)
}

func (p *downloadProgressReporter) finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.final = true
	p.mu.Unlock()
	p.add(0, true)
}

func (p *downloadProgressReporter) eventLocked(now time.Time) DownloadProgressEvent {
	elapsed := now.Sub(p.started).Seconds()
	var speed int64
	if elapsed > 0 {
		speed = int64(float64(p.downloaded) / elapsed)
	}
	// ETA from duration progress: the rate of playback-seconds fetched per
	// wall-clock second projects the remaining playback duration to a wall-clock
	// estimate. -1 marks it unknown (byte-only downloads, or before any segment).
	eta := int64(-1)
	if p.totalDuration > 0 && p.downloadedDuration > 0 && elapsed > 0 {
		rate := p.downloadedDuration / elapsed
		if rate > 0 {
			remaining := p.totalDuration - p.downloadedDuration
			if remaining < 0 {
				remaining = 0
			}
			eta = int64(remaining / rate)
		}
	}
	return DownloadProgressEvent{
		VideoID:           p.videoID,
		Path:              p.path,
		Itag:              p.itag,
		Part:              p.part,
		Downloaded:        p.downloaded,
		Total:             p.total,
		BytesPerSecond:    speed,
		DownloadedSeconds: p.downloadedDuration,
		TotalSeconds:      p.totalDuration,
		ETASeconds:        eta,
		Final:             p.final,
	}
}

func inferProgressPart(path string) string {
	switch {
	case strings.HasSuffix(path, ".video") || strings.Contains(path, ".video."):
		return "video"
	case strings.HasSuffix(path, ".audio") || strings.Contains(path, ".audio."):
		return "audio"
	default:
		return "media"
	}
}

var errThrottledDownload = errors.New("download speed below throttled-rate threshold")

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

func (r *rateLimitReader) Read(p []byte) (int, error) {
	maxChunk := r.bytesPerSecond / 10
	if maxChunk < 1 {
		maxChunk = 1
	}
	if int64(len(p)) > maxChunk {
		p = p[:maxChunk]
	}
	n, err := r.src.Read(p)
	if n > 0 {
		r.read += int64(n)
		elapsed := time.Since(r.start)
		target := time.Duration(float64(r.read) / float64(r.bytesPerSecond) * float64(time.Second))
		if target > elapsed {
			timer := time.NewTimer(target - elapsed)
			select {
			case <-r.ctx.Done():
				timer.Stop()
				return n, r.ctx.Err()
			case <-timer.C:
			}
		}
	}
	return n, err
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
	videoID string,
	requestHeaders http.Header,
	progress *downloadProgressReporter,
) (int64, error) {
	var file *os.File
	if err := retryFileAccessWithBackoff(ctx, cfg.FileAccessRetries, cfg.FileAccessBackoff, func() error {
		var err error
		file, err = openFile(outputPath, os.O_RDWR, 0o644)
		return err
	}); err != nil {
		return 0, err
	}
	defer file.Close()

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
			return 0, err
		}
		n, err := downloadRangeOnce(ctx, httpClient, streamURL, startOffset, file, videoID, requestHeaders, cfg, progress)
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
		if err := file.Truncate(startOffset); err != nil {
			return 0, err
		}
		if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
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
	videoID string,
	requestHeaders http.Header,
	cfg effectiveDownloadTransportConfig,
	progress *downloadProgressReporter,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	applyMediaRequestHeaders(req, requestHeaders, videoID)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		if progress != nil {
			progress.setTotal(totalFromContentRange(resp.Header.Get("Content-Range")))
		}
		// For range requests, the expected length is the size of the range
		// being fetched (end - start + 1), not the full Content-Length. We
		// derive it from the Content-Range header when available so a short
		// read is caught rather than silently accepted.
		copyCfg := cfg
		if expected := rangeSizeFromContentRange(resp.Header.Get("Content-Range"), startOffset); expected > 0 {
			copyCfg.expectedContentLength = expected
		}
		return copyWithDownloadConfig(ctx, w, resp.Body, copyCfg, progress)
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
	videoID string,
	requestHeaders http.Header,
	progress *downloadProgressReporter,
) (int64, error) {
	var file *os.File
	if err := retryFileAccessWithBackoff(ctx, cfg.FileAccessRetries, cfg.FileAccessBackoff, func() error {
		var err error
		file, err = createFile(outputPath)
		return err
	}); err != nil {
		return 0, err
	}
	defer file.Close()

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if err := file.Truncate(0); err != nil {
			return 0, err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
		if progress != nil {
			progress.reset()
		}
		n, err := downloadRangeOnce(ctx, httpClient, streamURL, 0, file, videoID, requestHeaders, cfg, progress)
		if errors.Is(err, errRangeNotSupported) {
			if progress != nil {
				progress.reset()
			}
			n, err = downloadURLToWriterOnceProgress(ctx, httpClient, streamURL, file, videoID, requestHeaders, cfg, progress)
		}
		if err == nil {
			return n, nil
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
	return 0, fmt.Errorf("full rewrite download failed with unknown retry error")
}

type effectiveDownloadTransportConfig struct {
	MaxRetries                  int
	InitialBackoff              time.Duration
	MaxBackoff                  time.Duration
	RetryStatusCodes            []int
	EnableChunked               bool
	ChunkSize                   int64
	MaxConcurrency              int
	RateLimitBytesPerSecond     int64
	ThrottledRateBytesPerSecond int64
	ThrottledRateMinDuration    time.Duration
	FileAccessRetries           int
	FileAccessBackoff           time.Duration
	// expectedContentLength is the expected total byte count for a single
	// non-chunked download, used to detect premature EOF. 0 disables the
	// check (used by chunked/range paths that verify completeness separately).
	expectedContentLength int64
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
		chunkSize = 10 << 20 // 10 MiB, matching yt-dlp's YouTube HTTPS chunk size
	}
	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	enableChunked := cfg.EnableChunked
	if !enableChunked && cfg.ChunkSize == 0 && cfg.MaxConcurrency == 0 {
		enableChunked = true
	}
	rateLimit := cfg.RateLimitBytesPerSecond
	if rateLimit < 0 {
		rateLimit = 0
	}
	if rateLimit > 0 {
		enableChunked = false
	}
	throttledRate := cfg.ThrottledRateBytesPerSecond
	if throttledRate < 0 {
		throttledRate = 0
	}
	throttledDuration := cfg.ThrottledRateMinDuration
	if throttledDuration <= 0 {
		throttledDuration = 3 * time.Second
	}
	if throttledRate > 0 {
		enableChunked = false
	}

	return effectiveDownloadTransportConfig{
		MaxRetries:                  maxRetries,
		InitialBackoff:              initialBackoff,
		MaxBackoff:                  maxBackoff,
		RetryStatusCodes:            statusCodes,
		EnableChunked:               enableChunked,
		ChunkSize:                   chunkSize,
		MaxConcurrency:              maxConcurrency,
		RateLimitBytesPerSecond:     rateLimit,
		ThrottledRateBytesPerSecond: throttledRate,
		ThrottledRateMinDuration:    throttledDuration,
		FileAccessRetries:           normalizeFileAccessRetries(cfg.FileAccessRetries),
		FileAccessBackoff:           cfg.FileAccessBackoff,
	}
}

func isChunkedMediaAccessDenied(err error) bool {
	status := downloadFailureHTTPStatus(err)
	return status == http.StatusUnauthorized || status == http.StatusForbidden
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

func downloadURLChunked(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	outputPath string,
	cfg effectiveDownloadTransportConfig,
	videoID string,
	requestHeaders http.Header,
	progress *downloadProgressReporter,
) (int64, error) {
	total, err := probeContentLengthWithRange(ctx, httpClient, streamURL, videoID, requestHeaders)
	if err != nil {
		return 0, err
	}
	if total <= 0 {
		return 0, errChunkProbeFailed
	}
	if progress != nil {
		progress.reset()
		progress.setTotal(total)
	}

	var file *os.File
	if err := retryFileAccessWithBackoff(ctx, cfg.FileAccessRetries, cfg.FileAccessBackoff, func() error {
		var err error
		file, err = createFile(outputPath)
		return err
	}); err != nil {
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

	completed := make([]bool, len(chunks))
	for i, chunk := range chunks {
		if ctx.Err() != nil {
			break
		}
		chunk := chunk
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if err := downloadChunkWithRetry(ctx, httpClient, streamURL, file, chunk[0], chunk[1], cfg, videoID, requestHeaders, progress); err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
				return
			}
			completed[idx] = true
		}()
	}

	wg.Wait()
	select {
	case err := <-errCh:
		return 0, err
	default:
		// Context cancellation can cause goroutines to return via the
		// <-ctx.Done() branch without writing their chunk and without
		// sending to errCh. If any chunk is missing, the file has
		// zero-filled gaps, so we must not report success.
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		for _, ok := range completed {
			if !ok {
				return 0, &TruncatedDownloadError{Expected: total, Actual: -1}
			}
		}
		return total, nil
	}
}

func probeContentLengthWithRange(
	ctx context.Context,
	httpClient *http.Client,
	streamURL string,
	videoID string,
	requestHeaders http.Header,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	applyMediaRequestHeaders(req, requestHeaders, videoID)
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
	total := totalFromContentRange(resp.Header.Get("Content-Range"))
	if total <= 0 {
		return 0, errChunkProbeFailed
	}
	return total, nil
}

func totalFromContentRange(raw string) int64 {
	cr := strings.TrimSpace(raw)
	slash := strings.LastIndex(cr, "/")
	if slash < 0 || slash == len(cr)-1 {
		return 0
	}
	var total int64
	if _, err := fmt.Sscanf(cr[slash+1:], "%d", &total); err != nil || total <= 0 {
		return 0
	}
	return total
}

// rangeSizeFromContentRange derives the expected byte count for a partial
// content response from its Content-Range header (e.g. "bytes 100-199/1000"
// yields 100). Returns 0 if the header is absent or unparseable, in which
// case the caller skips the premature-EOF check.
func rangeSizeFromContentRange(raw string, startOffset int64) int64 {
	cr := strings.TrimSpace(raw)
	if !strings.HasPrefix(cr, "bytes ") {
		return 0
	}
	body := strings.TrimPrefix(cr, "bytes ")
	slash := strings.LastIndex(body, "/")
	if slash < 0 {
		return 0
	}
	rangePart := body[:slash]
	dash := strings.Index(rangePart, "-")
	if dash < 0 {
		return 0
	}
	var end int64
	if _, err := fmt.Sscanf(rangePart[dash+1:], "%d", &end); err != nil || end < startOffset {
		return 0
	}
	return end - startOffset + 1
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
	videoID string,
	requestHeaders http.Header,
	progress *downloadProgressReporter,
) error {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		err := downloadChunkOnce(ctx, httpClient, streamURL, file, start, end, videoID, requestHeaders, progress)
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
	videoID string,
	requestHeaders http.Header,
	progress *downloadProgressReporter,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	applyMediaRequestHeaders(req, requestHeaders, videoID)
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
	downloaded := int64(0)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.WriteAt(buf[:n], offset); writeErr != nil {
				return writeErr
			}
			offset += int64(n)
			downloaded += int64(n)
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
	if progress != nil {
		progress.add(downloaded, false)
	}
	return nil
}

func defaultOutputPath(videoID string, title string, itag int, mimeType string, mode SelectionMode) string {
	if mode == SelectionModeMP3 {
		return defaultOutputPathForExt(videoID, title, strconv.Itoa(itag), "mp3")
	}
	ext := ".bin"
	if mediaType, _, err := mime.ParseMediaType(mimeType); err == nil {
		if parts := strings.SplitN(mediaType, "/", 2); len(parts) == 2 && parts[1] != "" {
			ext = "." + parts[1]
		}
	}
	return defaultOutputPathForExt(videoID, title, strconv.Itoa(itag), strings.TrimPrefix(ext, "."))
}

func defaultOutputPathForExt(videoID string, title string, itag string, ext string) string {
	return renderOutputPathTemplate(DefaultOutputTemplate, outputTemplateData{
		VideoID: videoID,
		Title:   title,
		Ext:     ext,
		Itag:    itag,
	})
}

type outputTemplateData struct {
	VideoID  string
	Title    string
	Uploader string
	Ext      string
	Itag     string
}

func renderOutputPathTemplate(template string, data outputTemplateData) string {
	values := map[string]string{
		"%(id)s":       sanitizeOutputToken(data.VideoID),
		"%(title)s":    sanitizeOutputToken(data.Title),
		"%(uploader)s": sanitizeOutputToken(data.Uploader),
		"%(ext)s":      sanitizeOutputToken(data.Ext),
		"%(itag)s":     sanitizeOutputToken(data.Itag),
	}
	rendered := template
	for token, value := range values {
		rendered = strings.ReplaceAll(rendered, token, value)
	}
	return rendered
}

func sanitizeOutputToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			b.WriteRune('_')
		default:
			if r < 32 {
				b.WriteRune('_')
				continue
			}
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "unknown"
	}
	// Strip trailing dots and spaces first, which Windows silently drops and
	// which can cause files to resolve to unexpected paths (e.g. "foo." -> "foo").
	out = strings.TrimRight(out, ". ")
	if out == "" {
		return "unknown"
	}
	// Collapse path traversal sequences so ../../etc becomes __etc. Applied
	// after trimming so a bare "." or ".." reduces to "" and falls through.
	out = strings.ReplaceAll(out, "..", "_")
	if out == "" || out == "." || out == ".." {
		return "unknown"
	}
	// Reject Windows reserved device names (CON, PRN, AUX, NUL, COM1-9,
	// LPT1-9) with any extension, since they cannot be used as filenames.
	if isWindowsReservedName(out) {
		return "unknown"
	}
	return out
}

// extension, and creating them can fail or have surprising effects.
func isWindowsReservedName(name string) bool {
	base := name
	if i := strings.LastIndex(name, "."); i >= 0 {
		base = name[:i]
	}
	base = strings.ToUpper(strings.TrimSpace(base))
	if base == "" {
		return false
	}
	reserved := map[string]bool{
		"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true,
		"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
		"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	}
	return reserved[base]
}

func detectOutputExt(mimeType string, mode SelectionMode) string {
	if mode == SelectionModeMP3 {
		return "mp3"
	}
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return "bin"
	}
	parts := strings.SplitN(mediaType, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "bin"
	}
	return parts[1]
}

func normalizedMergeOutputExt(raw string) string {
	ext := strings.ToLower(strings.TrimSpace(raw))
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		return "mp4"
	}
	for _, r := range ext {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return "mp4"
	}
	return ext
}

// defaultMergeExtForFormats resolves the merged-output container extension.
// When the caller requested an explicit merge-output format, that wins.
// Otherwise, when both selected formats are WebM (VP8/VP9/AV1 video + Opus
// audio), default to "webm" so the pure-Go puremux muxer can handle the
// merge without an FFmpeg binary. For any other codec pair, keep "mp4" so
// FFmpeg (when available) produces a broadly compatible container.
func defaultMergeExtForFormats(requested string, vidF, audF FormatInfo) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return normalizedMergeOutputExt(requested)
	}
	if isWebMMimeType(vidF.MimeType) && isWebMMimeType(audF.MimeType) {
		return "webm"
	}
	return normalizedMergeOutputExt("")
}

// isWebMMimeType reports whether a format MIME type describes a WebM
// container stream (video/webm or audio/webm). puremux remuxes these
// through its pure-Go pipeline without touching codec payloads.
func isWebMMimeType(mimeType string) bool {
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return strings.Contains(strings.ToLower(mimeType), "webm")
	}
	return mediaType == "video/webm" || mediaType == "audio/webm"
}

// hlsStreamTo downloads one or more HLS playlists in playlist order and writes
// the concatenated media to dst, carrying source media headers (e.g. SOOP
// Referer/Origin) and duration-based progress. It is the shared core of the
// file-backed downloadHLS and the `-o -` stdout path; progressPath tags the
// progress reporter ("-" for stdout).
func (c *Client) hlsStreamTo(ctx context.Context, videoID string, playlistURLs []string, dst io.Writer, progressPath string, format FormatInfo) error {
	headers := c.applySourceMediaHeaders(videoID, buildMediaRequestHeadersForSourceClient(c.config.RequestHeaders, videoID, format.SourceClient))
	transport := downloader.TransportConfig{
		MaxRetries:                  c.config.DownloadTransport.MaxRetries,
		InitialBackoff:              c.config.DownloadTransport.InitialBackoff,
		MaxBackoff:                  c.config.DownloadTransport.MaxBackoff,
		RetryStatusCodes:            append([]int(nil), c.config.DownloadTransport.RetryStatusCodes...),
		MaxConcurrency:              c.config.DownloadTransport.MaxConcurrency,
		SkipUnavailableFragments:    c.config.DownloadTransport.SkipUnavailableFragments,
		MaxSkippedFragments:         c.config.DownloadTransport.MaxSkippedFragments,
		ThrottledRateBytesPerSecond: c.config.DownloadTransport.ThrottledRateBytesPerSecond,
		ThrottledRateMinDuration:    c.config.DownloadTransport.ThrottledRateMinDuration,
	}

	// HLS has no upfront total size (segment sizes are unknown until fetched),
	// so report progress as an indeterminate download (bytes + speed, no
	// percent). Without this the CLI shows no activity for the whole HLS
	// download and appears frozen.
	reporter := newDownloadProgressReporter(c.config.OnDownloadProgress, videoID, progressPath, format.Itag, inferProgressPart(progressPath))
	if reporter != nil {
		dst = &progressCountingWriter{w: dst, rep: reporter}
		// HLS has no upfront byte total, but the source knows the media's total
		// playback duration (summed across parts for a multi-part VOD). Feeding
		// it in lets the downloader report a real percent and ETA from the
		// per-segment EXTINF durations instead of an indeterminate bar.
		if s, ok := c.getSession(videoID); ok && s.Info != nil && s.Info.DurationSec > 0 {
			reporter.setTotalDuration(float64(s.Info.DurationSec))
		}
	}
	// A multi-part VOD is downloaded part-by-part into the same writer. Each part
	// is an independent HLS playlist (its own segments and EXT-X-MAP init), so a
	// fresh downloader is used per part and the bodies are concatenated in order.
	var downloadErr error
	for _, url := range playlistURLs {
		dl := downloader.NewHLSDownloader(c.config.HTTPClient, url).
			WithRequestHeaders(headers).
			WithTransportConfig(transport)
		if reporter != nil {
			dl = dl.WithProgressCallback(reporter.addDuration)
		}
		if err := dl.Download(ctx, dst); err != nil {
			downloadErr = err
			break
		}
	}
	reporter.finish()
	return downloadErr
}

func (c *Client) downloadHLS(ctx context.Context, videoID string, playlistURLs []string, outputPath string, format FormatInfo, usePartFile bool) (*DownloadResult, error) {
	if len(playlistURLs) == 0 {
		return nil, fmt.Errorf("hls download: no playlist URL for itag=%d", format.Itag)
	}
	writePath := outputPath
	if usePartFile {
		writePath += ".part"
	}
	var f *os.File
	if err := retryFileAccess(ctx, c.config.DownloadTransport, func() error {
		var err error
		f, err = createFile(writePath)
		return err
	}); err != nil {
		return nil, err
	}

	downloadErr := c.hlsStreamTo(ctx, videoID, playlistURLs, f, outputPath, format)
	syncErr := f.Sync()
	closeErr := f.Close()
	if downloadErr != nil {
		return nil, downloadErr
	}
	if syncErr != nil {
		return nil, syncErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if usePartFile {
		if err := retryFileAccess(ctx, c.config.DownloadTransport, func() error {
			return renameFile(writePath, outputPath)
		}); err != nil {
			return nil, err
		}
	}

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

// progressCountingWriter forwards writes to w and reports the byte count to a
// download progress reporter. Used by the HLS/DASH segment writers, which have
// no total size, so the CLI still shows live throughput.
type progressCountingWriter struct {
	w   io.Writer
	rep *downloadProgressReporter
}

func (pw *progressCountingWriter) Write(b []byte) (int, error) {
	n, err := pw.w.Write(b)
	if n > 0 {
		pw.rep.add(int64(n), false)
	}
	return n, err
}

// dashStreamTo downloads a DASH representation and writes the media to dst with
// source media headers and progress. It is the shared core of the file-backed
// downloadDASH and the `-o -` stdout path; progressPath tags the reporter.
func (c *Client) dashStreamTo(ctx context.Context, videoID, streamURL string, dst io.Writer, progressPath string, format FormatInfo) error {
	repID := fmt.Sprintf("%d", format.Itag)
	headers := c.applySourceMediaHeaders(videoID, buildMediaRequestHeadersForSourceClient(c.config.RequestHeaders, videoID, format.SourceClient))
	transport := downloader.TransportConfig{
		MaxRetries:                  c.config.DownloadTransport.MaxRetries,
		InitialBackoff:              c.config.DownloadTransport.InitialBackoff,
		MaxBackoff:                  c.config.DownloadTransport.MaxBackoff,
		RetryStatusCodes:            append([]int(nil), c.config.DownloadTransport.RetryStatusCodes...),
		MaxConcurrency:              c.config.DownloadTransport.MaxConcurrency,
		SkipUnavailableFragments:    c.config.DownloadTransport.SkipUnavailableFragments,
		MaxSkippedFragments:         c.config.DownloadTransport.MaxSkippedFragments,
		ThrottledRateBytesPerSecond: c.config.DownloadTransport.ThrottledRateBytesPerSecond,
		ThrottledRateMinDuration:    c.config.DownloadTransport.ThrottledRateMinDuration,
	}
	dl := downloader.NewDASHDownloader(c.config.HTTPClient, streamURL, repID).
		WithRequestHeaders(headers).
		WithTransportConfig(transport)

	reporter := newDownloadProgressReporter(c.config.OnDownloadProgress, videoID, progressPath, format.Itag, inferProgressPart(progressPath))
	if reporter != nil {
		dst = &progressCountingWriter{w: dst, rep: reporter}
	}
	downloadErr := dl.Download(ctx, dst)
	reporter.finish()
	return downloadErr
}

func (c *Client) downloadDASH(ctx context.Context, videoID, streamURL, outputPath string, format FormatInfo, usePartFile bool) (*DownloadResult, error) {
	writePath := outputPath
	if usePartFile {
		writePath += ".part"
	}
	var f *os.File
	if err := retryFileAccess(ctx, c.config.DownloadTransport, func() error {
		var err error
		f, err = createFile(writePath)
		return err
	}); err != nil {
		return nil, err
	}

	downloadErr := c.dashStreamTo(ctx, videoID, streamURL, f, outputPath, format)
	syncErr := f.Sync()
	closeErr := f.Close()
	if downloadErr != nil {
		return nil, downloadErr
	}
	if syncErr != nil {
		return nil, syncErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if usePartFile {
		if err := retryFileAccess(ctx, c.config.DownloadTransport, func() error {
			return renameFile(writePath, outputPath)
		}); err != nil {
			return nil, err
		}
	}

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

func (c *Client) cleanupIntermediateFile(videoID, path string, keep bool) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if keep {
		c.emitDownloadEvent("cleanup", "skip", videoID, path, "keep_intermediate=true")
		return
	}
	c.emitDownloadEvent("cleanup", "delete", videoID, path, "")
	err := retryFileAccess(context.Background(), c.config.DownloadTransport, func() error {
		return removeFile(path)
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		c.emitDownloadEvent("cleanup", "failure", videoID, path, err.Error())
		return
	}
	c.emitDownloadEvent("cleanup", "complete", videoID, path, "")
}

func (c *Client) emitDownloadEvent(stage, phase, videoID, path, detail string) {
	if c == nil || c.config.OnDownloadEvent == nil {
		return
	}
	c.config.OnDownloadEvent(DownloadEvent{
		Stage:   stage,
		Phase:   phase,
		VideoID: videoID,
		Path:    path,
		Detail:  detail,
	})
}

func wrapDownloadFailure(err error, attempt AttemptDetail) error {
	if err == nil {
		return nil
	}
	return errors.Join(err, &DownloadFailureDetailError{
		Attempts: []AttemptDetail{attempt},
	})
}

func formatDownloadFailureDetail(attempt AttemptDetail) string {
	parts := []string{attempt.Reason}
	if attempt.HTTPStatus != 0 {
		parts = append(parts, fmt.Sprintf("http=%d", attempt.HTTPStatus))
	}
	if attempt.Protocol != "" {
		parts = append(parts, "proto="+attempt.Protocol)
	}
	if attempt.Itag != 0 {
		parts = append(parts, fmt.Sprintf("itag=%d", attempt.Itag))
	}
	if attempt.URLHost != "" {
		parts = append(parts, "host="+attempt.URLHost)
	}
	if attempt.URLHasN {
		parts = append(parts, "has_n=true")
	}
	if attempt.URLHasPOT {
		parts = append(parts, "has_pot=true")
	}
	if attempt.URLHasSignature {
		parts = append(parts, "has_sig=true")
	}
	if attempt.Client != "" {
		parts = append(parts, "client="+attempt.Client)
	}
	return strings.Join(parts, " ")
}

func downloadAttemptFromFormatAndURL(f types.FormatInfo, rawURL string, err error) AttemptDetail {
	d := AttemptDetail{
		Client:   f.SourceClient,
		Stage:    "download",
		Reason:   err.Error(),
		Itag:     f.Itag,
		Protocol: strings.TrimSpace(f.Protocol),
	}
	if d.Protocol == "" {
		d.Protocol = "unknown"
	}
	if u, parseErr := url.Parse(rawURL); parseErr == nil {
		d.URLHost = u.Host
		q := u.Query()
		d.URLHasN = q.Get("n") != ""
		d.URLHasPOT = q.Get("pot") != "" || strings.Contains(u.Path, "/pot/")
		d.URLHasSignature = q.Get("sig") != "" || q.Get("signature") != "" || q.Get("lsig") != ""
	}
	var statusErr *downloadHTTPStatusError
	if errors.As(err, &statusErr) {
		d.HTTPStatus = statusErr.StatusCode
	}
	return d
}
