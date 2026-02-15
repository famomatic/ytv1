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

	"github.com/mjmst/ytv1/client"
	"github.com/mjmst/ytv1/internal/playerjs"
)

func main() {
	var (
		videoID         = flag.String("v", "", "YouTube Video ID")
		proxy           = flag.String("proxy", "", "Proxy URL")
		download        = flag.Bool("download", false, "Download selected stream to file")
		itag            = flag.Int("itag", 0, "Target itag for download (default: mode-based)")
		mode            = flag.String("mode", "best", "Download mode: best|mp4av|mp4videoonly|audioonly|mp3")
		outputPath      = flag.String("o", "", "Output file path for download")
		clients         = flag.String("clients", "", "Comma-separated Innertube client order override (e.g. android_vr,web,web_safari)")
		visitorData     = flag.String("visitor-data", "", "VISITOR_INFO1_LIVE value override")
		playerJSURLOnly = flag.Bool("playerjs", false, "Print player base.js URL only")
	)
	flag.Parse()

	if *videoID == "" {
		fmt.Println("Usage: ytv1 -v <video_id> [-playerjs]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	httpClient := http.DefaultClient

	if *playerJSURLOnly {
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
	}
	if trimmed := strings.TrimSpace(*clients); trimmed != "" {
		cfg.ClientOverrides = splitCSV(trimmed)
	}
	c := client.New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if *download {
		fmt.Printf("Downloading video ID: %s...\n", *videoID)
		result, err := c.Download(ctx, *videoID, client.DownloadOptions{
			Itag:       *itag,
			Mode:       client.SelectionMode(*mode),
			OutputPath: *outputPath,
		})
		if err != nil {
			log.Fatalf("Error downloading stream: %v", err)
		}
		fmt.Printf("Downloaded: %s (%d bytes, itag=%d)\n", result.OutputPath, result.Bytes, result.Itag)
		return
	}

	fmt.Printf("Fetching info for video ID: %s...\n", *videoID)
	info, err := c.GetVideo(ctx, *videoID)
	if err != nil {
		log.Fatalf("Error fetching video info: %v", err)
	}

	fmt.Printf("Title: %s\n", info.Title)
	fmt.Printf("Found %d formats:\n", len(info.Formats))

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
