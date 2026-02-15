package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/muxer"
	"github.com/famomatic/ytv1/internal/playerjs"
)

func main() {
	var (
		videoID         = flag.String("v", "", "YouTube Video ID or Playlist ID")
		proxy           = flag.String("proxy", "", "Proxy URL")
		download        = flag.Bool("download", false, "Download selected stream to file")
		itag            = flag.Int("itag", 0, "Target itag for download (default: mode-based)")
		mode            = flag.String("mode", "best", "Download mode: best|mp4av|mp4videoonly|audioonly|mp3")
		outputPath      = flag.String("o", "", "Output file path for download")
		clients         = flag.String("clients", "", "Comma-separated Innertube client order override (e.g. android_vr,web,web_safari)")
		overrideDiag    = flag.Bool("override-diagnostics", false, "Print per-client attempt diagnostics on metadata failure")
		overrideAppend  = flag.Bool("override-append-fallback", false, "When -clients is set, keep fallback auto-append enabled")
		visitorData     = flag.String("visitor-data", "", "VISITOR_INFO1_LIVE value override")
		playerJSURLOnly = flag.Bool("playerjs", false, "Print player base.js URL only")
		listFormats     = flag.Bool("F", false, "List available formats")
		merge           = flag.Bool("merge", true, "Auto-merge video+audio if ffmpeg is available (default true)")
		ffmpegPath      = flag.String("ffmpeg-location", "", "Path to ffmpeg binary")
	)
	flag.Parse()

	if *videoID == "" {
		fmt.Println("Usage: ytv1 -v <video_id|playlist_id> [options]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	httpClient := http.DefaultClient
	
	// ... (playerJS logic omitted for brevity, keeping existing)
	if *playerJSURLOnly {
		// ... existing logic ...
		resolver := playerjs.NewResolver(httpClient, playerjs.NewMemoryCache())
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playerPath, err := resolver.GetPlayerURL(ctx, *videoID)
		if err != nil {
			log.Fatalf("Error resolving player URL: %v", err)
		}
		if strings.HasPrefix(playerPath, "http://") || strings.HasPrefix(playerPath, "https://") {
			fmt.Println(playerPath)
			return
		}
		fmt.Println("https://www.youtube.com" + playerPath)
		return
	}

	cfg := client.Config{
		ProxyURL:    *proxy,
		HTTPClient:  httpClient,
		VisitorData: *visitorData,
		Muxer:       muxer.NewFFmpegMuxer(*ffmpegPath),
	}
	if trimmed := strings.TrimSpace(*clients); trimmed != "" {
		cfg.ClientOverrides = splitCSV(trimmed)
		cfg.AppendFallbackOnClientOverrides = *overrideAppend
		if !*overrideAppend {
			cfg.DisableFallbackClients = true
		}
	}
	c := client.New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute) // Increased timeout for playlist/large downloads
	defer cancel()

	// Check if playlist
	if playlistID, err := client.ExtractPlaylistID(*videoID); err == nil && playlistID != "" {
		fmt.Printf("Fetching playlist: %s\n", playlistID)
		playlist, err := c.GetPlaylist(ctx, *videoID)
		if err != nil {
			log.Fatalf("Error fetching playlist: %v", err)
		}
		fmt.Printf("Playlist: %s (%d videos)\n", playlist.Title, len(playlist.Items))
		
		if *download {
			for i, item := range playlist.Items {
				fmt.Printf("[%d/%d] Downloading %s (%s)...\n", i+1, len(playlist.Items), item.Title, item.VideoID)
				_, err := c.Download(ctx, item.VideoID, client.DownloadOptions{
					Itag:        *itag,
					Mode:        client.SelectionMode(*mode),
					MergeOutput: *merge,
				})
				if err != nil {
					fmt.Printf("Failed to download %s: %v\n", item.VideoID, err)
				}
			}
		} else {
			// List items
			for i, item := range playlist.Items {
				fmt.Printf("%3d. %s (%s) [%s]\n", i+1, item.Title, item.VideoID, item.DurationSeconds)
			}
		}
		return
	}

	if *listFormats {
		info, err := c.GetVideo(ctx, *videoID)
		if err != nil {
			log.Fatalf("Error fetching video info: %v", err)
		}
		fmt.Printf("Title: %s\n", info.Title)
		fmt.Println("ID | Ext | Resolution | FPS | Bitrate | Size | Proto | Codec")
		fmt.Println("---|-----|------------|-----|---------|------|-------|------")
		for _, f := range info.Formats {
			fmt.Printf("%3d|%4s|%4dx%-4d|%3d|%6dk|%6s|%5s|%s\n",
				f.Itag, mimeExt(f.MimeType), f.Width, f.Height, f.FPS, f.Bitrate/1000, "-", f.Protocol, f.MimeType)
		}
		return
	}

	if *download {
		fmt.Printf("Downloading video ID: %s...\n", *videoID)
		result, err := c.Download(ctx, *videoID, client.DownloadOptions{
			Itag:        *itag,
			Mode:        client.SelectionMode(*mode),
			OutputPath:  *outputPath,
			MergeOutput: *merge,
		})
		if err != nil {
			if *overrideDiag {
				printAttemptDiagnostics(err)
			}
			log.Fatalf("Error downloading stream: %v", err)
		}
		fmt.Printf("Downloaded: %s (%d bytes, itag=%d)\n", result.OutputPath, result.Bytes, result.Itag)
		return
	}

	// Default: Fetch Info
	fmt.Printf("Fetching info for video ID: %s...\n", *videoID)
	info, err := c.GetVideo(ctx, *videoID)
	if err != nil {
		if *overrideDiag {
			printAttemptDiagnostics(err)
		}
		log.Fatalf("Error fetching video info: %v", err)
	}

	fmt.Printf("Title: %s\n", info.Title)
	fmt.Printf("Found %d formats:\n", len(info.Formats))
	// ... existing print ...

	for _, f := range info.Formats {
		fmt.Printf("[%d] %s (%dx%d) %d kbps - %s\n",
			f.Itag, f.QualityLabel, f.Width, f.Height, f.Bitrate/1000, f.MimeType)
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
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

func mimeExt(mimeType string) string {
	parts := strings.Split(mimeType, "/")
	if len(parts) < 2 {
		return "?"
	}
	sub := strings.Split(parts[1], ";")[0]
	return sub
}
