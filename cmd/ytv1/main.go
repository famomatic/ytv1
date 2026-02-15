package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
	"github.com/famomatic/ytv1/internal/playerjs"
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
			if opts.OverrideDiagnostics {
				printAttemptDiagnostics(err)
			}
		}
	}
}

func attachLifecycleHandlers(cfg *client.Config, opts cli.Options) {
	if !opts.Verbose {
		return
	}
	cfg.OnExtractionEvent = func(evt client.ExtractionEvent) {
		fmt.Println(formatExtractionEvent(evt))
	}
	cfg.OnDownloadEvent = func(evt client.DownloadEvent) {
		fmt.Println(formatDownloadEvent(evt))
	}
}

func processURL(ctx context.Context, c *client.Client, url string, opts cli.Options) error {
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

	info, err := c.GetVideo(ctx, url)
	if err != nil {
		return err
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
		return
	}
	fmt.Println("Attempt diagnostics:")
	for i, a := range attempts {
		fmt.Printf("  [%d] client=%s stage=%s", i+1, a.Client, a.Stage)
		if a.HTTPStatus != 0 {
			fmt.Printf(" http=%d", a.HTTPStatus)
		}
		if a.POTRequired {
			fmt.Printf(" pot_required=true")
		}
		if a.Reason != "" {
			fmt.Printf(" reason=%q", a.Reason)
		}
		fmt.Println()
	}
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
