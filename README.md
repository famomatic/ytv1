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

## Cycle B Substitute Scorecard (YouTube CLI Scope)

This cycle tracks operator-facing CLI substitute readiness rather than extractor internals only.

Current scorecard categories:

- `workflow_pass_rate`: percentage of defined workflows that pass in regression matrix
- `deterministic_output_rate`: repeated-run output path/result stability under same inputs
- `diagnosable_failure_rate`: failures that expose actionable category + hints

Workflow matrix classes:

- Single video default download (`best`)
- Selector-driven download (`-f` composite recipes)
- Playlist batch run with per-item summary
- Subtitle and mp3 common flow
- Archive-enabled rerun idempotency

Target threshold for substitute-ready claim:

- `workflow_pass_rate >= 95%`
- `deterministic_output_rate >= 99%`
- `diagnosable_failure_rate >= 99%`

The measured values are published during Cycle B closeout in `docs/IMPLEMENTATION_PLAN.md`.

Latest measured snapshot (`2026-02-16`):

- `workflow_pass_rate=100%`
- `deterministic_output_rate=100%`
- `diagnosable_failure_rate=100%`

Regression matrix commands (Cycle B):

- Fixture matrix (CI-safe): `go test ./...`
- Workflow-class fixture focus: `go test ./cmd/ytv1 -run TestWorkflowMatrix_FixtureCoverage -count=1`
- Live-gated matrix (YouTube endpoint): `YTV1_E2E=1 go test ./client -run TestE2E_ -count=1 -timeout 8m`

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

Output template tokens supported by `-o/--output`:

- `%(id)s`
- `%(title)s`
- `%(uploader)s`
- `%(ext)s`
- `%(itag)s`

Notes:

- Tokens are sanitized for filesystem safety on Windows/Linux style paths.
- The same input + options produce deterministic output paths.

Batch/control flags:

- `--abort-on-error`: stop processing remaining URLs after first failure
- `--no-continue`: disable resume of partial direct downloads
- `--download-archive FILE`: skip IDs already recorded in archive file and append newly completed downloads
- `--retries N`: override retry count for download/metadata transports
- `--retry-sleep-ms N`: override initial retry backoff in milliseconds
- `--write-subs`: write manual subtitles when available
- `--write-auto-subs`: prefer auto-generated subtitles for requested language(s)
- `--sub-lang a,b,c`: subtitle language priority list (default: `en`)

`--print-json` contract:

- Success: emits video metadata JSON object (`ok` field omitted for backward compatibility).
- Failure: emits JSON object with `ok=false`, `input`, `exit_code`, and `error{category,message,attempts?}`.

CLI exit code policy:

- `0`: success
- `1`: generic failure
- `2`: invalid input
- `3`: login required
- `4`: unavailable
- `5`: no playable formats
- `6`: challenge not solved
- `7`: all clients failed
- `8`: download failed
- `9`: mp3 transcoder not configured
- `10`: transcript parse failed

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
