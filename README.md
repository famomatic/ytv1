# ytv1

ytv1 is a Go-native library for extracting and downloading from YouTube. It provides APIs for video metadata, formats/stream URLs, downloads (including adaptive merge), playlists, and transcripts, without requiring a browser, Node.js, or a Python runtime.

`cmd/ytv1` is a thin CLI adapter on top of the library. The CLI is yt-dlp-inspired and focuses on practical YouTube workflows; it is not a general multi-site yt-dlp replacement.

## Docs

-   Architecture: `docs/ARCHITECTURE.md`
-   Implementation/track record: `docs/IMPLEMENTATION_PLAN.md`

## Requirements

-   Go: `1.23+` (see `go.mod`)
-   External tool: `ffmpeg` is required for merging separate video+audio streams and for MP3 transcoding
    -   If `ffmpeg` is not available on `PATH`, those flows will fail; direct single-stream downloads can still work.

## Why ytv1

-   **Go-native extraction pipeline**: player response fetching + signature/n challenge solving via `goja` (no external JS runtime).
-   **Package-first architecture**: the library is the product; `cmd/ytv1` is an adapter that only consumes `client` APIs/hooks.
-   **Operator workflow features**: practical `-f` selector support, deterministic `-o` output templates, playlist/batch controls, idempotent reruns via `--download-archive`, and structured/machine-readable outputs.
-   **Diagnostics you can automate**: verbose lifecycle events and stable JSON/exit-code behavior for scripting.

## Compared To `kkdai/youtube` and `yt-dlp`

This is not meant as a full feature checklist (both are moving targets), but a quick expectation-setter:

-   **vs `kkdai/youtube` (see `legacy/kkdai-youtube/README.md`)**: ytv1 is built around an Innertube/player-response + challenge-solving pipeline (instead of the older `get_video_info`-style approach) and ships a workflow-oriented CLI adapter.
-   **vs `yt-dlp`**: ytv1 is intentionally YouTube-scoped and implements a practical subset of yt-dlp CLI behaviors; it does not aim to replicate yt-dlp's multi-site coverage or its full post-processing ecosystem.

## CLI Compatibility (yt-dlp-inspired subset)

Commonly used flags supported by `cmd/ytv1` (not exhaustive; see `internal/cli/parser.go` for the complete list):

-   Format selection: `-f/--format`, `-F/--list-formats`
-   Output naming: `-o/--output`
-   Network/auth: `--proxy`, `--cookies`, `--visitor-data`
-   Batch control: `--abort-on-error`, `-i/--ignore-errors`, `--no-playlist/--yes-playlist`
-   Resume/retry: `--continue` (alias), `--no-continue`, `--retries`, `--retry-sleep-ms`
-   Archive/idempotency: `--download-archive`
-   Subtitles: `--write-subs`, `--write-auto-subs`, `--sub-lang/--sub-langs`, `--sub-format`, `--write-srt` (alias)
-   Playlist flat mode: `--flat-playlist/--extract-flat`
-   JSON output: `-j/-J/--dump-json/--print-json`, `--dump-single-json`

`--print-json` emits a yt-dlp-style single-entry payload on success; on failure it emits a structured error payload (for automation-stable diagnostics).

## Library (Package) Features

-   **Metadata Extraction**: rapid retrieval of video details, formats, and adaptive streams.
-   **Robust Downloading**: Built-in support for format selection, stream merging (video+audio), and retries.
-   **Playlist Support**: Efficient playlist iteration and metadata fetching.
-   **Transcripts/Subtitles**: Easy access to closed captions and auto-generated transcripts.
-   **Headless JS Execution**: Uses `goja` for executing YouTube's player JavaScript purely in Go, ensuring reliable signature deciphering without external runtimes (like Node.js) or browsers.
-   **Configurable**: Highly customizable transport policies, proxy support, and cache management.

## Library API (High-level)

Minimal contract (stable core):

-   `client.New(config)`
-   `client.GetVideo(ctx, input)`
-   `client.GetFormats(ctx, input)`
-   `client.ResolveStreamURL(ctx, videoID, itag)`

Frequently used additional APIs:

-   Downloads: `client.Download(ctx, input, options)`
-   Playlists: `client.GetPlaylist(ctx, input)`
-   Subtitles: `client.GetSubtitleTracks(ctx, input)`, `client.GetTranscript(ctx, input, languageCode)`
-   Manifests: `client.FetchDASHManifest(ctx, input)`, `client.FetchHLSManifest(ctx, input)`
-   Streaming: `client.OpenStream(ctx, input, options)`, `client.OpenFormatStream(ctx, input, itag)`

## Installation

Library:

```bash
go get github.com/famomatic/ytv1
```

CLI:

```bash
# Option A: build from this repo
go build -o ytv1 ./cmd/ytv1

# Option B: install via module path
go install github.com/famomatic/ytv1/cmd/ytv1@latest
```

## Known Gaps / Non-goals

-   **Multi-site support**: intentionally out of scope (YouTube-focused).
-   **Full yt-dlp feature parity**: many flags/behaviors are intentionally not implemented.
-   **Post-processing ecosystem**: beyond merge and basic MP3 transcoding, advanced yt-dlp postprocessors are not a goal.

## CLI Examples

```bash
# download best quality
./ytv1 https://www.youtube.com/watch?v=dQw4w9WgXcQ

# list formats
./ytv1 -F https://www.youtube.com/watch?v=dQw4w9WgXcQ

# choose a selector
./ytv1 -f "bv*+ba/b" https://www.youtube.com/watch?v=dQw4w9WgXcQ

# deterministic output naming
./ytv1 -o "%(title)s [%(id)s].%(ext)s" https://www.youtube.com/watch?v=dQw4w9WgXcQ

# playlist with idempotent reruns
./ytv1 --download-archive archive.txt https://www.youtube.com/playlist?list=PLxxxx

# subtitles
./ytv1 --write-subs --sub-lang "en,ko" --sub-format "srt" https://www.youtube.com/watch?v=dQw4w9WgXcQ

# tool-friendly JSON
./ytv1 -J https://www.youtube.com/watch?v=dQw4w9WgXcQ
```

## Library Usage (Minimal)

### Initialization

```go
package main

import (
	"context"
	"fmt"
	"github.com/famomatic/ytv1/client"
)

func main() {
	// standard configuration
	cfg := client.Config{}
	c := client.New(cfg)

	ctx := context.Background()
	info, err := c.GetVideo(ctx, "dQw4w9WgXcQ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s (%s)\n", info.Title, info.ID)
}
```
