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
		videoID = flag.String("v", "", "YouTube Video ID")
		proxy   = flag.String("proxy", "", "Proxy URL")
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
		ProxyURL: *proxy,
		HTTPClient: httpClient,
	}
	c := client.New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
