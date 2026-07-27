# ytv1

ytv1 is a Go-native library for extracting and downloading from YouTube (and, via a pluggable source seam, additional sites such as SOOP). It provides APIs for video metadata, formats/stream URLs, downloads (including adaptive merge), playlists, and transcripts, without requiring a browser, Node.js, or a Python runtime.

`cmd/ytv1` is a thin CLI adapter on top of the library. The CLI is yt-dlp-inspired and focuses on practical workflows; it is not a general multi-site yt-dlp replacement.

## Sources (Multi-site)

YouTube is the primary, fully-featured source. A pluggable seam
(`internal/source`) lets additional sites be added without touching the
YouTube path: a source implements `Matches(input)` + `Extract(ctx, input)`,
self-registers from its package `init()`, and returns normalized formats that
flow through the shared selection / parallel-download / mux pipeline. The
input URL is dispatched to the first matching source; anything unmatched falls
through to YouTube.

**SOOP** (`sooplive.co.kr`, formerly AfreecaTV) is supported for both VOD and
live:

-   VOD: `https://vod.sooplive.co.kr/player/<id>`
-   Live: `https://play.sooplive.co.kr/<bjid>/<bno>` (or `.../<bjid>`)

SOOP streams are HLS; the highest-quality master-playlist variant is selected
by default, and VOD segments are fetched in parallel
(`--concurrent-fragments` / `DownloadTransport.MaxConcurrency`). Site-specific
extraction (subtitles, transcripts, PO-token/signature logic) is
YouTube-only. SOOP CDN auth (the live `aid` token and `Referer`/`Origin`
headers) is handled automatically; live token expiry is re-resolved per
extraction. Anonymous CDN live is capped at 540p.

For higher-quality live (ORIGINAL/1080p) without a login, ytv1 can drive a
locally running SOOP desktop app ("SOOPStreamer") over its loopback P2P grid
socket and remux the pulled stream to MPEG-TS on the fly. This engages
automatically when the app is running (set `YTV1_SOOP_NO_AGENT=1` to force the
CDN path); if the agent delivers no media it falls back to the CDN. Stream it
straight to a player with `ytv1 <live-url> -o -` (media on stdout, all status on
stderr).

## Docs

-   Architecture: `docs/ARCHITECTURE.md`
-   Implementation/track record: `docs/IMPLEMENTATION_PLAN.md`

## Requirements

-   Go: `1.23+` (see `go.mod`)
-   External tool: `ffmpeg` is optional. Merging WebM (VP8/VP9/AV1 + Opus) video+audio streams is handled by the pure-Go [`puremux`](https://github.com/famomatic/puremux) muxer with no binary dependency.
    -   `ffmpeg` is still required for MP4/MKV output with non-WebM codecs, metadata embedding (`--embed-metadata`), and MP3 transcoding.
    -   If `ffmpeg` is not available on `PATH` and the selected formats require it, those flows fail; direct single-stream and WebM-pair downloads still work.

## Why ytv1

-   **Go-native extraction pipeline**: player response fetching + signature/n challenge solving via `goja` (no external JS runtime).
-   **Package-first architecture**: the library is the product; `cmd/ytv1` is an adapter that only consumes `client` APIs/hooks.
-   **Operator workflow features**: practical `-f` selector support, deterministic `-o` output templates, playlist/batch controls, idempotent reruns via `--download-archive`, and structured/machine-readable outputs.
-   **Diagnostics you can automate**: verbose lifecycle events and stable JSON/exit-code behavior for scripting.

## Compared To `kkdai/youtube` and `yt-dlp`

This is not meant as a full feature checklist (both are moving targets), but a quick expectation-setter:

-   **vs `kkdai/youtube`**: ytv1 is built around an Innertube/player-response + challenge-solving pipeline (instead of the older `get_video_info`-style approach) and ships a workflow-oriented CLI adapter.
-   **vs `yt-dlp`**: ytv1 is intentionally YouTube-scoped and implements a practical subset of yt-dlp CLI behaviors; it does not aim to replicate yt-dlp's multi-site coverage or its full post-processing ecosystem.

## Package-First Usage

The CLI flags are adapters over package configuration. Use `client.Config` directly when embedding ytv1:

```go
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/famomatic/ytv1/client"
)

func main() {
	c := client.New(client.Config{
		ProxyURL:           "http://127.0.0.1:3128",
		SourceAddress:      "0.0.0.0",
		InsecureSkipVerify: false,
		RequestTimeout:     30 * time.Second,
		RequestHeaders: http.Header{
			"User-Agent": []string{"my-app/1.0"},
			"Referer":    []string{"https://www.youtube.com/"},
		},
		DownloadTransport: client.DownloadTransportConfig{
			MaxRetries:               10,
			InitialBackoff:           time.Second,
			MaxConcurrency:           4,
			SkipUnavailableFragments: true,
			ThrottledRateBytesPerSecond: 100 * 1024,
			FileAccessRetries:        3,
			FileAccessBackoff:        10 * time.Millisecond,
		},
		MetadataTransport: client.MetadataTransportConfig{
			MaxRetries:     3,
			InitialBackoff: time.Second,
		},
	})

	_, _ = c.GetVideo(context.Background(), "jNQXAC9IVRw")
}
```

## CLI Compatibility (yt-dlp-inspired subset)

Commonly used flags supported by `cmd/ytv1` (not exhaustive; see `internal/cli/parser.go` for the complete list):

-   Format selection: `-f/--format`, `-F/--list-formats`
-   Output naming: `-P/--paths`, `-o/--output`, `--id`, `--restrict-filenames/--no-restrict-filenames`, `--trim-filenames/--trim-file-names`, `-w/--no-overwrites`, `--force-overwrites/--yes-overwrites/--no-force-overwrites`, `--write-info-json/--no-write-info-json`, `--write-description/--no-write-description`, `--write-playlist-metafiles/--no-write-playlist-metafiles`, `--write-link/--write-url-link/--write-webloc-link/--write-desktop-link`, `--write-thumbnail/--no-write-thumbnail`, `--get-title`, `--get-id`, `--get-description`, `--get-duration`, `--get-filename`, `--get-format`, `-g/--get-url`, `--get-thumbnail`
-   Metadata printing: `-O/--print FIELD_OR_TEMPLATE`, `--print-to-file TEMPLATE FILE`
-   Network/auth: `--proxy`, `--socket-timeout`, `--source-address`, `-4/--force-ipv4`, `-6/--force-ipv6`, `--no-check-certificates`, `--user-agent`, `--referer`, `--add-headers`, `--cookies`, `--visitor-data`
-   Input: `-a/--batch-file`, `--no-batch-file`
-   Simulation: `-s/--simulate/--no-simulate`, `--skip-download/--no-download`
-   Output control: `-q/--quiet/--no-quiet`, `--no-warnings`, `--no-progress`, `--progress`, `--newline`
-   Batch control: `--abort-on-error`, `-i/--ignore-errors`, `--max-downloads`, `--skip-playlist-after-errors`, `--no-playlist/--yes-playlist`, `-I/--playlist-items`, `--playlist-start/--playlist-end`, `--playlist-reverse`, `--playlist-random`
-   Resume/retry: `--continue` (alias), `--no-continue`, `--part/--no-part`, `--mtime/--no-mtime`, `-r/--limit-rate/--rate-limit`, `--buffer-size`, `--http-chunk-size`, `--resize-buffer/--no-resize-buffer`, `-N/--concurrent-fragments`, `--skip-unavailable-fragments/--abort-on-unavailable-fragments`, `--sleep-requests`, `--sleep-interval/--min-sleep-interval`, `--max-sleep-interval`, `--sleep-subtitles`, `--retries`, `--file-access-retries`, `--fragment-retries`, `--extractor-retries`, `--retry-sleep`, `--retry-sleep-ms`
-   Post-processing: `--merge-output-format`, `--remux-video`, `-k/--keep-video`, `--no-keep-video`, `--post-overwrites/--no-post-overwrites`, `-x/--extract-audio`, `--audio-format`, `--audio-quality`, `--embed-metadata/--add-metadata`, `--no-embed-metadata/--no-add-metadata`, `--postprocessor-args/--ppa`
-   Archive/idempotency: `--download-archive`, `--no-download-archive`, `--break-on-existing`, `--force-write-archive`
-   Subtitles: `--list-subs`, `--all-subs`, `--write-subs/--no-write-subs`, `--write-auto-subs/--write-automatic-subs`, `--no-write-auto-subs`, `--sub-lang/--sub-langs/--srt-langs`, `--sub-format/--convert-subs`, `--write-srt/--no-write-srt`
-   Playlist flat mode: `--flat-playlist/--extract-flat`
-   JSON output: `-j/-J/--dump-json/--print-json`, `--dump-single-json`

`--print-json` emits a yt-dlp-style single-entry payload on success; on failure it emits a structured error payload (for automation-stable diagnostics).

`--playlist-items` accepts item selectors such as `1,3:5,:10,1:10:2,12-15,-3:-1,::-1`.
Playlist output templates can use `%(playlist_id)s`, `%(playlist_title)s`, `%(playlist_index)s`, `%(playlist_autonumber)s`, `%(playlist_count)s`, `%(playlist_uploader)s`, `%(playlist_uploader_id)s`, `%(playlist_channel)s`, and `%(playlist_channel_id)s` in addition to regular video fields such as `%(id)s`, `%(title)s`, `%(uploader)s`, `%(uploader_id)s`, `%(channel)s`, `%(channel_id)s`, `%(ext)s`, `%(itag)s`, `%(format_id)s`, `%(protocol)s`, `%(vcodec)s`, `%(acodec)s`, `%(resolution)s`, `%(width)s`, `%(height)s`, `%(fps)s`, `%(tbr)s`, `%(vbr)s`, `%(abr)s`, `%(upload_date)s`, `%(release_date)s`, and `%(timestamp)s`; `--print`/`--print-to-file` also support `%(webpage_url)s` and `%(original_url)s`.
`--sub-langs all` expands available subtitle tracks, and simple exclusions such as `--sub-langs all,-live_chat` or `--sub-langs en,ko,-ko` are supported.

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
