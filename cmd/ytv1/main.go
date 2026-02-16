package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
	"github.com/famomatic/ytv1/internal/playerjs"
)

var verboseLifecyclePrinter *lifecyclePrinter

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
	attachLifecycleHandlers(&cfg, opts)
	c := client.New(cfg)
	ctx := context.Background()

	// Process each URL
	for _, url := range opts.URLs {
		if err := processURL(ctx, c, url, opts); err != nil {
			// Print error but don't exit if multiple URLs?
			// yt-dlp prints "ERROR: [youtube] ... " and continues unless --abort-on-error
			// We print to stderr
			log.Printf("Error processing %s: %v", url, err)
			if opts.OverrideDiagnostics || opts.Verbose {
				printAttemptDiagnostics(err)
			}
		}
	}
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

	if opts.SkipDownload {
		fmt.Printf("Skipping download for %s\n", info.Title)
		return nil
	}

	// Download
	// Map format selector to client mode
	mode := client.SelectionModeBest
	switch opts.FormatSelector {
	case "best", "bestvideo+bestaudio":
		mode = client.SelectionModeBest
	case "bestaudio", "audioonly":
		mode = client.SelectionModeAudioOnly
	case "bestvideo", "videoonly":
		mode = client.SelectionModeVideoOnly
	case "mp4":
		mode = client.SelectionModeMP4AV
	case "mp3":
		mode = client.SelectionModeMP3
	default:
		// Fallback to best for unknown selectors for now
		// TODO: Implement full selector parser
		if strings.HasPrefix(opts.FormatSelector, "res:") {
			// Basic resolution parser?
		}
	}

	fmt.Printf("Downloading: %s [%s]\n", info.Title, info.ID)
	res, err := c.Download(ctx, url, client.DownloadOptions{
		Mode:        mode,
		OutputPath:  opts.OutputTemplate, // Client handles templating slightly different, usually expects strict path or ""
		MergeOutput: true,                // Always try to merge on 'best'
	})
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
	return nil
}

func processPlaylist(ctx context.Context, c *client.Client, playlistID string, opts cli.Options) error {
	fmt.Printf("Fetching playlist: %s\n", playlistID)
	playlist, err := c.GetPlaylist(ctx, playlistID)
	if err != nil {
		return err
	}
	fmt.Printf("Playlist: %s (%d videos)\n", playlist.Title, len(playlist.Items))

	// If ListFormats/PrintJSON, maybe apply to each item?
	// yt-dlp applies flags to every item.

	for i, item := range playlist.Items {
		fmt.Printf("[%d/%d] Processing %s (%s)...\n", i+1, len(playlist.Items), item.Title, item.VideoID)
		if err := processURL(ctx, c, item.VideoID, opts); err != nil {
			log.Printf("Failed to process %s: %v", item.VideoID, err)
		}
	}
	return nil
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
	switch {
	case errors.Is(err, client.ErrInvalidInput):
		fmt.Println("hint: unsupported input. Use a full YouTube URL or 11-char video ID, then retry.")
	case errors.Is(err, client.ErrLoginRequired):
		fmt.Println("hint: login-required content. Retry with --cookies <netscape.txt> and --visitor-data <VISITOR_INFO1_LIVE>.")
	case errors.Is(err, client.ErrNoPlayableFormats):
		fmt.Println("hint: no playable formats. Retry with -F to inspect candidates and --verbose for extraction stages.")
	case errors.Is(err, client.ErrChallengeNotSolved):
		fmt.Println("hint: challenge solve failed. Retry with --verbose and inspect [extract] challenge:* logs.")
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
