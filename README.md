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
- Extraction/download lifecycle hooks (`OnExtractionEvent`, `OnDownloadEvent`)
- Merge intermediate retention policy (`KeepIntermediateFiles`)

## yt-dlp Parity Matrix (youtube extractor scope)

| Area | Status | Notes |
|---|---|---|
| Multi-client player API attempts | Ported | Per-client attempt diagnostics and deterministic ordering |
| Watch page + player JS discovery | Ported | Watch-page scrape and base.js fetch |
| JS `n` challenge rewrite | Ported | Cached solver and URL rewrite paths |
| JS signature decipher (`s`) | Ported | Cached solver and signatureCipher materialization |
| DASH/HLS manifest expansion | Ported | Manifest fetch + normalized format candidates |
| Download/merge lifecycle | Ported | Destination/start/merge/cleanup events and keep/delete policy |
| Playlist/transcript/subtitles | Partial | Core APIs exist; long-tail parity remains |
| Login/captcha/account flows | Deferred | Explicitly outside current v1 migration scope |

## Lifecycle Events

- `OnExtractionEvent`: emits `webpage`, `player_api_json`, `player_js`, `challenge`, `manifest` stage events.
- `OnDownloadEvent`: emits destination/start/complete/failure, merge, and cleanup events.
- CLI (`ytv1.exe`) maps these events to stdout when `--verbose` is enabled.

Example:

```bash
ytv1.exe --verbose -f best https://youtu.be/DSYFmhjDbvs
```

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

- `ytv1.exe --verbose <video_id>`
- `ytv1.exe --verbose --override-diagnostics <video_id>`
- `ytv1.exe --verbose --clients android_vr,web,web_safari <video_id>`
- `ytv1.exe --verbose --visitor-data <VISITOR_INFO1_LIVE> <video_id>`
- `ytv1.exe --verbose --po-token <POT_TOKEN> <video_id>`
- `ytv1.exe --playerjs <video_id>`

## Troubleshooting

- `login required`: provide cookies (`--cookies`) and/or visitor context (`--visitor-data`), then retry with `--override-diagnostics`.
- `missing required POT`: provide a token via `--po-token` (CLI) or configure `client.Config.PoTokenProvider` (library API).
- `challenge not solved`: retry with `--verbose` and check `player_js`/`challenge` stages; player JS may have changed.
- `no playable formats`: use `-F` to inspect candidates and verify manifest extraction stages in verbose logs.
- merge output missing: verify ffmpeg availability or pass `--ffmpeg-location`.
