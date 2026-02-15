package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/mjmst/ytv1/client"
)

func main() {
	var (
		videoID = flag.String("v", "", "YouTube Video ID")
		proxy   = flag.String("proxy", "", "Proxy URL")
	)
	flag.Parse()

	if *videoID == "" {
		fmt.Println("Usage: ytv1 -v <video_id>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	cfg := client.Config{
		ProxyURL: *proxy,
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
