package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
	"github.com/famomatic/ytv1/internal/playerjs"
)

var verboseLifecyclePrinter *lifecyclePrinter
var activeDownloadArchive *downloadArchive

const (
	exitCodeSuccess             = 0
	exitCodeGenericFailure      = 1
	exitCodeInvalidInput        = 2
	exitCodeLoginRequired       = 3
	exitCodeUnavailable         = 4
	exitCodeNoPlayableFormats   = 5
	exitCodeChallengeUnresolved = 6
	exitCodeAllClientsFailed    = 7
	exitCodeDownloadFailed      = 8
	exitCodeMP3ConfigRequired   = 9
	exitCodeTranscriptParse     = 10
)

func main() {
	opts := cli.ParseFlags()

	if len(opts.URLs) == 0 {
		fmt.Println("Usage: ytv1 [OPTIONS] URL [URL...]")
		// We don't exit 1 if help or version was requested, but ParseFlags handles Help auto-exit.
		// If explicit Help flag wasn't handled by standard flag package (it usually is), we might descend here.
		// Assuming standard flag behavior: Exit 0 on -h.
		// If we are here, no URLs were provided.
		os.Exit(1)
	}

	cfg, err := cli.ToClientConfig(opts)
	if err != nil {
		log.Fatalf("Failed to initialize config: %v", err)
	}
	if strings.TrimSpace(opts.DownloadArchive) != "" {
		archive, err := newDownloadArchive(opts.DownloadArchive)
		if err != nil {
			log.Fatalf("Failed to initialize download archive: %v", err)
		}
		activeDownloadArchive = archive
		defer func() {
			if err := archive.Close(); err != nil {
				log.Printf("failed to close download archive: %v", err)
			}
		}()
	}
	attachLifecycleHandlers(&cfg, opts)
	c := client.New(cfg)
	ctx := context.Background()
	exitCode := processInputsWithExitCode(ctx, c, opts.URLs, opts, processURL)
	if exitCode != exitCodeSuccess {
		os.Exit(exitCode)
	}
}

func processInputs(
	ctx context.Context,
	c *client.Client,
	urls []string,
	opts cli.Options,
	processor func(context.Context, *client.Client, string, cli.Options) error,
) bool {
	return processInputsWithExitCode(ctx, c, urls, opts, processor) != exitCodeSuccess
}

func processInputsWithExitCode(
	ctx context.Context,
	c *client.Client,
	urls []string,
	opts cli.Options,
	processor func(context.Context, *client.Client, string, cli.Options) error,
) int {
	exitCode := exitCodeSuccess
	for _, url := range urls {
		if err := processor(ctx, c, url, opts); err != nil {
			code := classifyExitCode(err)
			if code > exitCode {
				exitCode = code
			}
			if opts.PrintJSON {
				emitJSONFailure(url, err, code)
			} else {
				log.Printf("Error processing %s: %v", url, err)
			}
			if (opts.OverrideDiagnostics || opts.Verbose) && !opts.PrintJSON {
				printAttemptDiagnostics(err)
			}
			if opts.AbortOnError {
				break
			}
		}
	}
	return exitCode
}

func attachLifecycleHandlers(cfg *client.Config, opts cli.Options) {
	if !opts.Verbose {
		return
	}
	lp := newLifecyclePrinter(time.Now)
	verboseLifecyclePrinter = lp
	cfg.OnExtractionEvent = func(evt client.ExtractionEvent) {
		fmt.Println(lp.formatExtractionEvent(evt))
	}
	cfg.OnDownloadEvent = func(evt client.DownloadEvent) {
		fmt.Println(lp.formatDownloadEvent(evt))
	}
}

func processURL(ctx context.Context, c *client.Client, url string, opts cli.Options) error {
	totalStart := time.Now()
	// 1. Check if it is a playlist
	// For now, treat everything as video unless we want to support playlists explicitly here
	// client.GetVideo handles video IDs.
	// Check prompt for playlist ID extraction
	if playlistID, err := client.ExtractPlaylistID(url); err == nil && playlistID != "" {
		return processPlaylist(ctx, c, playlistID, opts)
	}

	if opts.PlayerJSURLOnly {
		return handlePlayerJS(ctx, c, url)
	}
	if shouldSkipDownloadByArchive(url) {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	extractStart := time.Now()
	info, err := c.GetVideo(ctx, url)
	if err != nil {
		if opts.Verbose {
			fmt.Println(formatExtractionEvent(client.ExtractionEvent{
				Stage:  "total",
				Phase:  "failure",
				Client: "all",
				Detail: fmt.Sprintf("elapsed_ms=%d", time.Since(extractStart).Milliseconds()),
			}))
		}
		return err
	}
	extractMs := time.Since(extractStart).Milliseconds()
	if opts.Verbose {
		fmt.Println(formatExtractionEvent(client.ExtractionEvent{
			Stage:  "total",
			Phase:  "complete",
			Client: "all",
			Detail: fmt.Sprintf("elapsed_ms=%d", extractMs),
		}))
	}

	if opts.PrintJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if opts.ListFormats {
		printFormats(info)
		return nil // yt-dlp stops after listing formats
	}

	if opts.WriteSubs || opts.WriteAutoSubs {
		if err := writeRequestedSubtitles(ctx, c, url, info, opts); err != nil {
			return err
		}
	}

	if opts.SkipDownload {
		fmt.Printf("Skipping download for %s\n", info.Title)
		return nil
	}

	fmt.Printf("Downloading: %s [%s]\n", info.Title, info.ID)
	res, err := c.Download(ctx, url, buildDownloadOptions(opts))
	if err != nil {
		return err
	}
	fmt.Printf("Downloaded to: %s\n", res.OutputPath)
	if opts.Verbose && verboseLifecyclePrinter != nil {
		timing := verboseLifecyclePrinter.popVideoTiming(info.ID)
		videoMs := timing.downloadVideoMs
		audioMs := timing.downloadAudioMs
		if videoMs == 0 && audioMs == 0 && timing.downloadSingleMs > 0 {
			videoMs = timing.downloadSingleMs
		}
		downloadTotalMs := videoMs + audioMs
		avgSpeed := "0B/s"
		if downloadTotalMs > 0 {
			bps := int64(float64(res.Bytes) / (float64(downloadTotalMs) / 1000.0))
			avgSpeed = fmt.Sprintf("%dB/s", bps)
		}
		fmt.Printf(
			"total_elapsed_ms=%d extract_ms=%d download_ms(video/audio)=%d/%d merge_ms=%d final_size=%d avg_speed=%s\n",
			time.Since(totalStart).Milliseconds(),
			extractMs,
			videoMs,
			audioMs,
			timing.mergeMs,
			res.Bytes,
			avgSpeed,
		)
	}
	if err := recordCompletedDownload(info.ID); err != nil {
		return err
	}
	return nil
}

func buildDownloadOptions(opts cli.Options) client.DownloadOptions {
	downloadOpts := client.DownloadOptions{
		Mode:        client.SelectionModeBest,
		OutputPath:  opts.OutputTemplate, // Client handles templating slightly different, usually expects strict path or ""
		MergeOutput: true,                // Always try to merge on 'best'
		Resume:      !opts.NoContinue,
	}

	raw := strings.TrimSpace(opts.FormatSelector)
	lower := strings.ToLower(raw)
	switch lower {
	case "", "best":
		return downloadOpts
	case "bestvideo+bestaudio":
		downloadOpts.FormatSelector = "bestvideo+bestaudio/best"
		return downloadOpts
	case "bestaudio", "audioonly":
		downloadOpts.Mode = client.SelectionModeAudioOnly
		return downloadOpts
	case "bestvideo", "videoonly":
		downloadOpts.Mode = client.SelectionModeVideoOnly
		return downloadOpts
	case "mp4":
		downloadOpts.Mode = client.SelectionModeMP4AV
		return downloadOpts
	case "mp3":
		downloadOpts.Mode = client.SelectionModeMP3
		return downloadOpts
	}

	if itag, err := strconv.Atoi(lower); err == nil && itag > 0 {
		downloadOpts.Itag = itag
		return downloadOpts
	}

	downloadOpts.FormatSelector = raw
	return downloadOpts
}

func processPlaylist(ctx context.Context, c *client.Client, playlistID string, opts cli.Options) error {
	fmt.Printf("Fetching playlist: %s\n", playlistID)
	playlist, err := c.GetPlaylist(ctx, playlistID)
	if err != nil {
		return err
	}
	fmt.Printf("Playlist: %s (%d videos)\n", playlist.Title, len(playlist.Items))

	summary, failures := runPlaylistItems(ctx, c, playlist.Items, opts, processURL)
	fmt.Printf(
		"Playlist summary: total=%d succeeded=%d failed=%d aborted=%t\n",
		summary.Total,
		summary.Succeeded,
		summary.Failed,
		summary.Aborted,
	)
	if len(failures) > 0 {
		for _, failure := range failures {
			log.Printf("Failed to process %s: %v", failure.VideoID, failure.Err)
		}
		return fmt.Errorf("playlist completed with failures: failed=%d/%d", summary.Failed, summary.Total)
	}
	return nil
}

type playlistRunSummary struct {
	Total     int
	Succeeded int
	Failed    int
	Aborted   bool
}

type playlistItemFailure struct {
	VideoID string
	Err     error
}

func runPlaylistItems(
	ctx context.Context,
	c *client.Client,
	items []client.PlaylistItem,
	opts cli.Options,
	processor func(context.Context, *client.Client, string, cli.Options) error,
) (playlistRunSummary, []playlistItemFailure) {
	summary := playlistRunSummary{Total: len(items)}
	failures := make([]playlistItemFailure, 0)
	for i, item := range items {
		fmt.Printf("[%d/%d] Processing %s (%s)...\n", i+1, len(items), item.Title, item.VideoID)
		if err := processor(ctx, c, item.VideoID, opts); err != nil {
			summary.Failed++
			failures = append(failures, playlistItemFailure{
				VideoID: item.VideoID,
				Err:     err,
			})
			if opts.AbortOnError {
				summary.Aborted = true
				break
			}
			continue
		}
		summary.Succeeded++
	}
	return summary, failures
}

func printFormats(info *client.VideoInfo) {
	fmt.Printf("Title: %s\n", info.Title)
	fmt.Println("ID | Ext | Resolution | FPS | Bitrate | Proto | Codec")
	fmt.Println("---|-----|------------|-----|---------|-------|------")
	for _, f := range info.Formats {
		fmt.Printf("%3d|%4s|%4dx%-4d|%3d|%6dk|%5s|%s\n",
			f.Itag, mimeExt(f.MimeType), f.Width, f.Height, f.FPS, f.Bitrate/1000, f.Protocol, f.MimeType)
	}
}

func mimeExt(mimeType string) string {
	parts := strings.Split(mimeType, "/")
	if len(parts) < 2 {
		return "?"
	}
	sub := strings.Split(parts[1], ";")[0]
	return sub
}

func handlePlayerJS(ctx context.Context, c *client.Client, videoID string) error {
	resolver := playerjs.NewResolver(nil, playerjs.NewMemoryCache())
	path, err := resolver.GetPlayerURL(ctx, videoID)
	if err != nil {
		return err
	}
	if strings.HasPrefix(path, "http") {
		fmt.Println(path)
	} else {
		fmt.Println("https://www.youtube.com" + path)
	}
	return nil
}

func writeRequestedSubtitles(
	ctx context.Context,
	c *client.Client,
	input string,
	info *client.VideoInfo,
	opts cli.Options,
) error {
	langs := parseSubtitleLanguages(opts.SubLangs)
	if len(langs) == 0 {
		langs = []string{"en"}
	}

	written := 0
	failures := make([]string, 0, len(langs))
	for _, lang := range langs {
		transcript, err := c.GetTranscript(ctx, input, lang)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s(%v)", lang, err))
			continue
		}
		outputPath := subtitleOutputPath(opts.OutputTemplate, info, transcript.LanguageCode)
		if err := writeTranscriptAsSRT(outputPath, transcript); err != nil {
			failures = append(failures, fmt.Sprintf("%s(%v)", transcript.LanguageCode, err))
			continue
		}
		written++
		fmt.Printf("Written subtitle: %s\n", outputPath)
	}

	if written == 0 && len(failures) > 0 {
		return fmt.Errorf("failed to write subtitles: %s", strings.Join(failures, "; "))
	}
	if len(failures) > 0 {
		log.Printf("subtitle partial failure: %s", strings.Join(failures, "; "))
	}
	return nil
}

func parseSubtitleLanguages(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		lang := strings.ToLower(strings.TrimSpace(part))
		if lang == "" {
			continue
		}
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	return out
}

func subtitleOutputPath(outputTemplate string, info *client.VideoInfo, lang string) string {
	safeLang := strings.TrimSpace(strings.ToLower(lang))
	if safeLang == "" {
		safeLang = "unknown"
	}
	if strings.TrimSpace(outputTemplate) == "" {
		return fmt.Sprintf("%s.%s.srt", info.ID, safeLang)
	}
	base := strings.TrimSpace(outputTemplate)
	base = strings.ReplaceAll(base, "%(id)s", sanitizeTemplateToken(info.ID))
	base = strings.ReplaceAll(base, "%(title)s", sanitizeTemplateToken(info.Title))
	base = strings.ReplaceAll(base, "%(uploader)s", sanitizeTemplateToken(info.Author))
	base = strings.ReplaceAll(base, "%(ext)s", "srt")
	base = strings.ReplaceAll(base, "%(itag)s", "subs_"+safeLang)
	if strings.TrimSpace(base) == "" {
		return fmt.Sprintf("%s.%s.srt", info.ID, safeLang)
	}
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base + "." + safeLang + ".srt"
}

func sanitizeTemplateToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
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
	return out
}

func writeTranscriptAsSRT(path string, transcript *client.Transcript) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for i, entry := range transcript.Entries {
		start := formatSRTTimestamp(entry.StartSec)
		end := formatSRTTimestamp(entry.StartSec + entry.DurSec)
		if _, err := fmt.Fprintf(f, "%d\n%s --> %s\n%s\n\n", i+1, start, end, strings.TrimSpace(entry.Text)); err != nil {
			return err
		}
	}
	return nil
}

func formatSRTTimestamp(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	totalMs := int64(math.Round(sec * 1000))
	hours := totalMs / (60 * 60 * 1000)
	minutes := (totalMs / (60 * 1000)) % 60
	seconds := (totalMs / 1000) % 60
	millis := totalMs % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, millis)
}

func shouldSkipDownloadByArchive(input string) bool {
	if activeDownloadArchive == nil {
		return false
	}
	videoID, err := client.ExtractVideoID(input)
	if err != nil {
		return false
	}
	if !activeDownloadArchive.Has(videoID) {
		return false
	}
	fmt.Printf("Skipping (in archive): %s\n", videoID)
	return true
}

func recordCompletedDownload(videoID string) error {
	if activeDownloadArchive == nil {
		return nil
	}
	if err := activeDownloadArchive.Add(videoID); err != nil {
		return fmt.Errorf("failed to update download archive: %w", err)
	}
	return nil
}

func printAttemptDiagnostics(err error) {
	attempts, ok := client.AttemptDetails(err)
	if !ok || len(attempts) == 0 {
		printGenericRemediationHints(err)
		return
	}
	fmt.Println("Attempt diagnostics:")
	for i, a := range attempts {
		fmt.Printf("  [%d] client=%s stage=%s", i+1, a.Client, a.Stage)
		if a.Itag != 0 {
			fmt.Printf(" itag=%d", a.Itag)
		}
		if a.Protocol != "" {
			fmt.Printf(" proto=%s", a.Protocol)
		}
		if a.HTTPStatus != 0 {
			fmt.Printf(" http=%d", a.HTTPStatus)
		}
		if a.URLHost != "" {
			fmt.Printf(" host=%s", a.URLHost)
		}
		if a.URLHasN {
			fmt.Printf(" has_n=true")
		}
		if a.URLHasPOT {
			fmt.Printf(" has_pot=true")
		}
		if a.URLHasSignature {
			fmt.Printf(" has_sig=true")
		}
		if a.POTRequired {
			fmt.Printf(" pot_required=true")
		}
		if a.Reason != "" {
			fmt.Printf(" reason=%q", a.Reason)
		}
		fmt.Println()
	}
	for _, hint := range remediationHintsForAttempts(attempts) {
		fmt.Println(hint)
	}
}

func printGenericRemediationHints(err error) {
	var noPlayableDetail *client.NoPlayableFormatsDetailError
	switch {
	case errors.Is(err, client.ErrInvalidInput):
		fmt.Println("hint: unsupported input. Use a full YouTube URL or 11-char video ID, then retry.")
	case errors.Is(err, client.ErrLoginRequired):
		fmt.Println("hint: login-required content. Retry with --cookies <netscape.txt> and --visitor-data <VISITOR_INFO1_LIVE>.")
	case errors.Is(err, client.ErrNoPlayableFormats):
		if errors.As(err, &noPlayableDetail) && noPlayableDetail.Selector != "" {
			fmt.Printf("hint: selector %q matched no formats (%s). Retry with -F and adjust -f expression.\n", noPlayableDetail.Selector, noPlayableDetail.SelectionError)
			return
		}
		fmt.Println("hint: no playable formats. Retry with -F to inspect candidates and --verbose for extraction stages.")
	case errors.Is(err, client.ErrChallengeNotSolved):
		fmt.Println("hint: challenge solve failed. Retry with --verbose and inspect [extract] challenge:* logs.")
	case errors.Is(err, client.ErrMP3TranscoderNotConfigured):
		fmt.Println("hint: mp3 mode requires an MP3 transcoder. Configure client.Config.MP3Transcoder (CLI: use a build with transcoder wiring).")
	default:
		fmt.Println("hint: retry with --verbose --override-diagnostics to inspect stage/client failure details.")
	}
}

func remediationHintsForAttempts(attempts []client.AttemptDetail) []string {
	var hints []string
	sawLogin := false
	sawPOTRequired := false
	sawMissingPOT := false
	sawNoN := false
	sawHTTP403 := false
	sawHTTP429 := false

	for _, a := range attempts {
		if a.LoginRequired {
			sawLogin = true
		}
		if a.POTRequired {
			sawPOTRequired = true
			if !a.POTAvailable {
				sawMissingPOT = true
			}
		}
		if !a.URLHasN {
			sawNoN = true
		}
		if a.HTTPStatus == 403 {
			sawHTTP403 = true
		}
		if a.HTTPStatus == 429 {
			sawHTTP429 = true
		}
	}

	if sawLogin {
		hints = append(hints, "hint: login-required restriction detected. Retry with --cookies <netscape.txt> and, if needed, --visitor-data <VISITOR_INFO1_LIVE>.")
	}
	if sawPOTRequired && sawMissingPOT {
		hints = append(hints, "hint: missing required POT detected. Supply --po-token <token> or configure client.Config.PoTokenProvider.")
	}
	if sawHTTP429 {
		hints = append(hints, "hint: upstream throttling (HTTP 429). Retry later or use lower-concurrency network settings.")
	}
	if sawHTTP403 && sawNoN {
		hints = append(hints, "hint: 403 + missing n-signature observed. Retry with --verbose and verify [extract] challenge:success logs.")
	}
	if len(hints) == 0 {
		hints = append(hints, "hint: retry with --verbose --override-diagnostics to inspect client/stage-specific failure details.")
	}
	return hints
}

func formatExtractionEvent(evt client.ExtractionEvent) string {
	scope := evt.Stage + ":" + evt.Phase
	if evt.Client != "" {
		scope += " client=" + evt.Client
	}
	if evt.Detail != "" {
		scope += " detail=" + evt.Detail
	}
	return "[extract] " + scope
}

func formatDownloadEvent(evt client.DownloadEvent) string {
	scope := evt.Stage + ":" + evt.Phase
	if evt.VideoID != "" {
		scope += " video_id=" + evt.VideoID
	}
	if evt.Path != "" {
		scope += " path=" + evt.Path
	}
	if evt.Detail != "" {
		scope += " detail=" + evt.Detail
	}
	return "[download] " + scope
}

type lifecyclePrinter struct {
	now func() time.Time
	mu  sync.Mutex
	// key: stage|client
	extractStarts map[string]time.Time
	// key: stage|videoID|path
	downloadStarts map[string]time.Time
	// key: videoID
	videoTimings map[string]videoTiming
}

func newLifecyclePrinter(now func() time.Time) *lifecyclePrinter {
	return &lifecyclePrinter{
		now:            now,
		extractStarts:  make(map[string]time.Time),
		downloadStarts: make(map[string]time.Time),
		videoTimings:   make(map[string]videoTiming),
	}
}

type videoTiming struct {
	downloadVideoMs  int64
	downloadAudioMs  int64
	downloadSingleMs int64
	mergeMs          int64
}

func (p *lifecyclePrinter) formatExtractionEvent(evt client.ExtractionEvent) string {
	detail := evt.Detail
	key := evt.Stage + "|" + evt.Client

	p.mu.Lock()
	switch evt.Phase {
	case "start":
		p.extractStarts[key] = p.now()
	case "success", "failure", "partial", "complete":
		if started, ok := p.extractStarts[key]; ok {
			detail = appendDetail(detail, fmt.Sprintf("elapsed_ms=%d", p.now().Sub(started).Milliseconds()))
			delete(p.extractStarts, key)
		}
	}
	p.mu.Unlock()

	return formatExtractionEvent(client.ExtractionEvent{
		Stage:  evt.Stage,
		Phase:  evt.Phase,
		Client: evt.Client,
		Detail: detail,
	})
}

func (p *lifecyclePrinter) formatDownloadEvent(evt client.DownloadEvent) string {
	detail := evt.Detail
	key := evt.Stage + "|" + evt.VideoID + "|" + evt.Path
	now := p.now()

	p.mu.Lock()
	switch evt.Phase {
	case "start", "delete":
		p.downloadStarts[key] = now
	case "complete", "failure", "skip":
		if started, ok := p.downloadStarts[key]; ok {
			elapsed := now.Sub(started).Milliseconds()
			detail = appendDetail(detail, fmt.Sprintf("elapsed_ms=%d", elapsed))
			if evt.Stage == "download" && evt.Phase == "complete" {
				if bytes, ok := extractBytesFromDetail(detail); ok && elapsed > 0 {
					seconds := float64(elapsed) / 1000.0
					speedBPS := float64(bytes) / seconds
					speedMiB := speedBPS / (1024.0 * 1024.0)
					detail = appendDetail(detail, fmt.Sprintf("speed_bps=%d", int64(speedBPS)))
					detail = appendDetail(detail, fmt.Sprintf("speed_mib_s=%.2f", speedMiB))
				}
				role := inferDownloadRole(evt.Path)
				detail = appendDetail(detail, "part="+role)
				vt := p.videoTimings[evt.VideoID]
				switch role {
				case "video":
					vt.downloadVideoMs += elapsed
				case "audio":
					vt.downloadAudioMs += elapsed
				default:
					vt.downloadSingleMs += elapsed
				}
				p.videoTimings[evt.VideoID] = vt
			}
			if evt.Stage == "merge" && evt.Phase == "complete" {
				vt := p.videoTimings[evt.VideoID]
				vt.mergeMs += elapsed
				p.videoTimings[evt.VideoID] = vt
			}
			delete(p.downloadStarts, key)
		}
	}
	p.mu.Unlock()

	return formatDownloadEvent(client.DownloadEvent{
		Stage:   evt.Stage,
		Phase:   evt.Phase,
		VideoID: evt.VideoID,
		Path:    evt.Path,
		Detail:  detail,
	})
}

func (p *lifecyclePrinter) popVideoTiming(videoID string) videoTiming {
	p.mu.Lock()
	defer p.mu.Unlock()
	vt := p.videoTimings[videoID]
	delete(p.videoTimings, videoID)
	return vt
}

func appendDetail(base string, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return extra
	}
	if strings.Contains(base, extra) {
		return base
	}
	return base + " " + extra
}

func extractBytesFromDetail(detail string) (int64, bool) {
	tokens := strings.FieldsFunc(detail, func(r rune) bool {
		return r == ' ' || r == ','
	})
	for _, token := range tokens {
		if !strings.HasPrefix(token, "bytes=") {
			continue
		}
		raw := strings.TrimPrefix(token, "bytes=")
		v, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && v >= 0 {
			return v, true
		}
	}
	return 0, false
}

func inferDownloadRole(path string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".video"):
		return "video"
	case strings.HasSuffix(lower, ".audio"):
		return "audio"
	default:
		return "single"
	}
}

type cliErrorReport struct {
	OK       bool           `json:"ok"`
	Input    string         `json:"input"`
	ExitCode int            `json:"exit_code"`
	Error    cliErrorDetail `json:"error"`
}

type cliErrorDetail struct {
	Category string                 `json:"category"`
	Message  string                 `json:"message"`
	Attempts []client.AttemptDetail `json:"attempts,omitempty"`
}

func emitJSONFailure(input string, err error, exitCode int) {
	report := cliErrorReport{
		OK:       false,
		Input:    input,
		ExitCode: exitCode,
		Error: cliErrorDetail{
			Category: string(client.ClassifyError(err)),
			Message:  err.Error(),
		},
	}
	if attempts, ok := client.AttemptDetails(err); ok && len(attempts) > 0 {
		report.Error.Attempts = attempts
	}
	_ = json.NewEncoder(os.Stdout).Encode(report)
}

func classifyExitCode(err error) int {
	switch client.ClassifyError(err) {
	case client.ErrorCategoryInvalidInput:
		return exitCodeInvalidInput
	case client.ErrorCategoryLoginRequired:
		return exitCodeLoginRequired
	case client.ErrorCategoryUnavailable:
		return exitCodeUnavailable
	case client.ErrorCategoryNoPlayableFormats:
		return exitCodeNoPlayableFormats
	case client.ErrorCategoryChallengeNotSolved:
		return exitCodeChallengeUnresolved
	case client.ErrorCategoryAllClientsFailed:
		return exitCodeAllClientsFailed
	case client.ErrorCategoryDownloadFailed:
		return exitCodeDownloadFailed
	case client.ErrorCategoryMP3TranscoderNotConfigured:
		return exitCodeMP3ConfigRequired
	case client.ErrorCategoryTranscriptParse:
		return exitCodeTranscriptParse
	default:
		return exitCodeGenericFailure
	}
}

type downloadArchive struct {
	path string
	file *os.File
	mu   sync.Mutex
	ids  map[string]struct{}
}

func newDownloadArchive(path string) (*downloadArchive, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("archive path is empty")
	}
	if dir := filepath.Dir(cleanPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	archive := &downloadArchive{
		path: cleanPath,
		file: f,
		ids:  make(map[string]struct{}),
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if _, err := client.ExtractVideoID(line); err != nil {
			continue
		}
		archive.ids[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_ = f.Close()
		return nil, err
	}
	return archive, nil
}

func (a *downloadArchive) Close() error {
	if a == nil || a.file == nil {
		return nil
	}
	return a.file.Close()
}

func (a *downloadArchive) Has(videoID string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.ids[videoID]
	return ok
}

func (a *downloadArchive) Add(videoID string) error {
	if a == nil {
		return nil
	}
	if _, err := client.ExtractVideoID(videoID); err != nil {
		return fmt.Errorf("invalid video id for archive: %q", videoID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.ids[videoID]; exists {
		return nil
	}
	if _, err := a.file.WriteString(videoID + "\n"); err != nil {
		return err
	}
	if err := a.file.Sync(); err != nil {
		return err
	}
	a.ids[videoID] = struct{}{}
	return nil
}
