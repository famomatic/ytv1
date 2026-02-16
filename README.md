# ytv1

ytv1 is a powerful and flexible Go library for interacting with YouTube. It provides a robust API for extracting video metadata, downloading streams, fetching playlists, and retrieving transcripts, all without requiring an external browser or heavy dependencies.

A command-line interface (CLI) is also provided as a reference implementation and usage tool.

## Library Features

-   **Metadata Extraction**: rapid retrieval of video details, formats, and adaptive streams.
-   **Robust Downloading**: Built-in support for format selection, stream merging (video+audio), and retries.
-   **Playlist Support**: Efficient playlist iteration and metadata fetching.
-   **Transcripts/Subtitles**: Easy access to closed captions and auto-generated transcripts.
-   **Headless JS Execution**: Uses `goja` for executing YouTube's player JavaScript purely in Go, ensuring reliable signature deciphering without external runtimes (like Node.js) or browsers.
-   **Configurable**: Highly customizable transport policies, proxy support, and cache management.

## Installation

```bash
go get github.com/famomatic/ytv1
```

## Library Usage

### Initialization

```go
package main

import (
	"github.com/famomatic/ytv1/client"
)

func main() {
	// standard configuration
	cfg := client.Config{} 
	c := client.New(cfg)
}
```

### Fetch Video Metadata

```go
ctx := context.Background()
videoID := "dQw4w9WgXcQ"

info, err := c.GetVideo(ctx, videoID)
if err != nil {
    panic(err)
}

fmt.Printf("Title: %s\n", info.Title)
fmt.Printf("Author: %s\n", info.Author)
fmt.Printf("Duration: %d seconds\n", info.DurationSec)

// Iterate formats
for _, f := range info.Formats {
    fmt.Printf("Format %d: %s (%dx%d)\n", f.Itag, f.MimeType, f.Width, f.Height)
}
```

### Download Video

```go
options := client.DownloadOptions{
    Mode:        client.SelectionModeBest, // or SelectionModeAudioOnly, SelectionModeVideoOnly
    OutputPath:  "video.mp4",
    MergeOutput: true, // Automatically merge video+audio using ffmpeg if needed
}

res, err := c.Download(ctx, videoID, options)
if err != nil {
    panic(err)
}

fmt.Printf("Downloaded to %s (%d bytes)\n", res.OutputPath, res.Bytes)
```

### Fetch Playlist

```go
playlistID := "PL..."
playlist, err := c.GetPlaylist(ctx, playlistID)
if err != nil {
    panic(err)
}

fmt.Printf("Playlist: %s (%d items)\n", playlist.Title, len(playlist.Items))
for _, item := range playlist.Items {
    fmt.Println(item.Title)
}
```

### Get Transcript (Subtitles)

```go
// Fetch English transcript
transcript, err := c.GetTranscript(ctx, videoID, "en")
if err != nil {
    panic(err)
}

for _, entry := range transcript.Entries {
    fmt.Printf("[%f] %s\n", entry.StartSec, entry.Text)
}
```

## CLI Tool

The project includes a CLI wrapper that demonstrates the library's capabilities and serves as a `yt-dlp` compatible downloader.

### Installation

```bash
go build -o ytv1 ./cmd/ytv1
```

### Basic Usage

```bash
# Download best quality
./ytv1 https://www.youtube.com/watch?v=dQw4w9WgXcQ

# List formats
./ytv1 -F https://www.youtube.com/watch?v=dQw4w9WgXcQ

# Download audio only
./ytv1 -f "bestaudio" https://www.youtube.com/watch?v=dQw4w9WgXcQ
```
