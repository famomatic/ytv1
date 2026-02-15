# ytv1

`ytv1` is a Go-native, package-first YouTube extraction library.

It is designed for backend and tooling use cases where you need stable extraction behavior without a Python runtime dependency.

## Why ytv1

- Library-first architecture (`client` is the product, CLI is only an adapter)
- Go-native extraction pipeline with minimal external runtime dependencies
- Player JS decipher and `n` parameter rewriting support
- Manifest-aware format discovery (direct + DASH + HLS candidates)
- Typed error details for robust caller-side handling
- Stream-first APIs for non-file consumers (`io.ReadCloser`)

## Install

```bash
go get github.com/famomatic/ytv1
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/famomatic/ytv1/client"
)

func main() {
	c := client.New(client.Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	video, err := c.GetVideo(ctx, "jNQXAC9IVRw")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("title=%q formats=%d\n", video.Title, len(video.Formats))
}
```

## Public API

Core API:

- `client.New(config)`
- `(*Client).GetVideo(ctx, input)`
- `(*Client).GetFormats(ctx, input)`
- `(*Client).ResolveStreamURL(ctx, videoID, itag)`

Additional package APIs:

- `(*Client).FetchDASHManifest(ctx, input)`
- `(*Client).FetchHLSManifest(ctx, input)`
- `(*Client).Download(ctx, input, options)`
- `(*Client).OpenStream(ctx, input, options)`
- `(*Client).OpenFormatStream(ctx, input, itag)`
- `(*Client).GetSubtitleTracks(ctx, input)`
- `(*Client).GetTranscript(ctx, input, languageCode)`
- `(*Client).GetPlaylist(ctx, input)`

## Configuration Highlights

`client.Config` supports:

- Request transport and proxy control (`HTTPClient`, `ProxyURL`, headers, timeout)
- Innertube client behavior (`ClientOverrides`, `ClientSkip`, fallback control)
- Player JS fetch behavior (base URL, headers, user agent, locale)
- PO token support and policy matrix (`PoTokenProvider`, `PoTokenFetchPolicy`)
- Download and metadata retry/backoff tuning
- Session cache bounds (`SessionCacheTTL`, `SessionCacheMaxEntries`)
- Subtitle track selection policy
- Optional package warning logger (`Logger`)

## Error Handling

Package-level sentinel errors:

- `client.ErrInvalidInput`
- `client.ErrUnavailable`
- `client.ErrLoginRequired`
- `client.ErrNoPlayableFormats`
- `client.ErrChallengeNotSolved`
- `client.ErrAllClientsFailed`
- `client.ErrMP3TranscoderNotConfigured`
- `client.ErrTranscriptParse`

Use `errors.Is` for sentinel checks and `errors.As` for typed details such as:

- `InvalidInputDetailError`
- `NoPlayableFormatsDetailError`
- `AllClientsFailedDetailError`
- `LoginRequiredDetailError`
- `UnavailableDetailError`
- `TranscriptUnavailableDetailError`
- `TranscriptParseDetailError`

## Project Layout

- `client/*`: public package API and orchestration glue
- `internal/*`: extraction internals (innertube, policy, orchestrator, formats, playerjs, challenge)
- `cmd/ytv1`: thin CLI adapter for smoke testing
- `docs/*`: architecture and implementation planning

## CLI

The CLI exists for verification and debugging, not as the primary product surface.

Examples:

- `ytv1.exe -v <video_id>`
- `ytv1.exe -v <video_id> -playerjs`
- `ytv1.exe -v <video_id> -download [-itag <itag>] [-o <output_path>]`
