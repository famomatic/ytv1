# ytv1 Implementation Plan (Package-First)

## Status Legend

- `[x]` done
- `[-]` in progress
- `[ ]` pending

## Current Snapshot (Update Every Work Session)

### Immediate Next Tasks (Execution Order)

1. `[x]` Rebuild format parser normalization (`internal/formats/parser.go`) from Innertube field matrix:
   - Parse fps/quality/protocol/container/codec/audio-channel metadata deterministically.
   - Preserve decipher-required fields explicitly (`Ciphered`, raw cipher params) instead of "assume URL".
   - Add fixture tests for mixed `formats/adaptiveFormats`, missing fields, live/offline variants.
2. `[x]` Replace "best first" download selection with explicit selection policy:
   - Add package mode enum: `Best`, `MP4AV`, `MP4VideoOnly`, `AudioOnly`, `MP3`.
   - Build deterministic selector (container, hasAudio/hasVideo, bitrate/resolution tie-breakers).
   - Keep `itag` override as highest priority.
3. `[x]` Implement MP3 pipeline as optional transcode layer:
   - Add `Transcoder` interface in package config (default nil).
   - If mode=`MP3` and no transcoder configured, return typed error.
   - Stream download -> transcode to output writer/path (no temp shell command hard dependency in core).
4. `[x]` Refactor PO token handling to yt-dlp-like per-protocol/per-format decision:
   - Evaluate POT requirement at stream-protocol stage (HTTPS/DASH/HLS), not only request stage.
   - Add provider fetch policy hooks: required/recommended/never.
   - Emit structured skip reasons when formats are dropped due to missing POT.
5. `[x]` Expand package error contract from coarse sentinels to typed detail errors:
   - Add attempt matrix payload (client, stage, status, reason, http code, pot requirement).
   - Keep sentinel compatibility via `errors.Is`, expose rich detail via `errors.As`.
6. `[x]` Add resilient download transport features (package-level):
   - Range-based chunk download with bounded concurrency and context cancellation.
   - Retry/backoff for transient HTTP/network failures.
   - Optional resume support when output exists.
7. `[x]` Add package APIs for playlist/transcript/subtitle extraction:
   - `GetPlaylist`, `GetTranscript`, subtitle track listing/fetch.
   - Reuse Innertube context/policy stack (no CLI-only logic).
8. `[x]` Expand client config surface to match package use-cases:
   - Per-request headers, retry policy, timeout strategy, client skip/priority policy, POT strategy, selector knobs.
   - No hidden hardcoded defaults without override path.
9. `[x]` Strengthen playerjs robustness and regression strategy:
   - Add real `base.js` fixture rotation workflow and parser fallback patterns.
   - Add CI tests for signature/n-function extraction across multiple captured player revisions.
10. `[x]` Harden input normalization:
   - Support broader YouTube URL families and query combinations.
   - Keep strict invalid-input typed errors with exact reason.
11. `[x]` Keep explicit override investigation open:
   - Reproduce `-clients android_vr,web,web_safari` -> `login required`.
   - Capture per-client attempt diagnostics and decide fallback insertion policy for override mode.
12. `[x]` Harden playlist continuation token selection:
   - Avoid stopping at 100 items when non-playlist continuation tokens are present.
   - Follow valid continuation chain(s) until exhausted.
   - Add regression test for mixed valid/invalid continuation tokens.

### Implementation Logic (Gap Closure)

1. Format Parser Logic
   - Move all field extraction to a single normalization table keyed by raw Innertube keys.
   - Derive `HasAudio/HasVideo` from codec/channel/dimension signals instead of one-field heuristics.
   - Normalize protocol source:
     - direct URL => `https`
     - dash manifest entry => `dash`
     - hls manifest entry => `hls`
   - Keep unresolved/ciphered entries in output with explicit flags; never silently drop.

2. Selection Logic
   - Introduce ranking pipeline:
     - filter by mode constraints
     - hard filters: cipher solvable, protocol allowed, container allowed
     - score by quality/bitrate/fps/audio presence depending on mode
   - Return `ErrNoPlayableFormats` with mode-specific reason if filtered to zero.

3. Download/Transcode Logic
   - Core downloader returns stream reader abstraction + metadata.
   - Output writer layer handles file/path concerns.
   - MP3 mode delegates to configured transcoder:
     - input: audio stream + source mime/container
     - output: encoded mp3 bytes to destination
   - Keep transcoder pluggable to avoid mandatory external binary dependency.

4. PO Token Logic
   - Separate "request POT" and "stream POT" policy checks.
   - Track POT state per client/protocol in attempt context.
   - On missing recommended POT: keep format but mark warning.
   - On missing required POT: skip format, continue other candidates.

5. Error Mapping Logic
   - Add internal error taxonomy:
     - request failure, playability failure, pot-gated skip, decipher failure, transport failure
   - Public mapping:
     - sentinel error for compatibility
     - attach typed detail struct for diagnostics and callers.

6. Test Logic
   - Add table tests for selector modes and tie-break rules.
   - Add integration-like tests with recorded player responses covering:
     - progressive-only
     - adaptive-only
     - cipher-required
     - login-required fallback
   - Add download tests for:
     - chunk retries
     - cancel propagation
     - resume path
     - mp3 missing-transcoder error.

## 1. Positioning

- `ytv1` is a Go library first.
- CLI (`cmd/ytv1`) is only a thin adapter for manual smoke tests.
- Success criteria are package-level API behavior and testability, not executable behavior.

## 2. Primary Deliverable

Provide a stable public package API:

- `client.New(config)`
- `client.GetVideo(ctx, input)`
- `client.GetFormats(ctx, input)`
- `client.ResolveStreamURL(ctx, videoID, itag)`

CLI is explicitly non-authoritative. If CLI works but package API is unstable, milestone is considered failed.

## 3. References and Porting Policy

### 3.1 Source references

- `legacy/kkdai-youtube`: transport/parsing reference only.
- `d:/yt-dlp/yt_dlp/extractor/youtube`: extraction strategy reference.

### 3.2 Porting rule

- Port behavior, not structure.
- No Python-style state flow in public package API.
- Keep runtime dependencies pure Go.

## 4. Package Architecture

- `client` (public)
  - public API, options, error contracts
- `internal/orchestrator`
  - client fallback orchestration
- `internal/innertube`
  - client registry, request builders, response types
- `internal/policy`
  - candidate client selection logic
- `internal/playerjs`
  - player JS fetch/cache + decipher helpers
- `internal/formats`
  - streamingData parsing + normalization
- `internal/challenge`
  - `s` / `n` challenge solve interfaces
- `internal/httpx`
  - shared HTTP abstraction

## 5. Public API Contract (v1)

### 5.1 Config

- `HTTPClient *http.Client`
- `Logger` (optional)
- `PoTokenProvider` (optional interface, no hard dependency)
- `ClientOverrides []string` (optional, for debug/testing)

### 5.2 Data contract

- `VideoInfo`
  - `ID`, `Title`, `DurationSec`, `Author`
  - `Formats []FormatInfo`
- `FormatInfo`
  - `Itag`, `MimeType`, `HasAudio`, `HasVideo`
  - `Bitrate`, `Width`, `Height`, `FPS`
  - `URL` (if directly playable)
  - `Ciphered bool`

### 5.3 Error contract

Typed errors (package-level):

- `ErrUnavailable`
- `ErrLoginRequired`
- `ErrNoPlayableFormats`
- `ErrChallengeNotSolved`
- `ErrAllClientsFailed` (with attempt details)

## 6. Execution Pipeline (Internal)

1. Normalize input -> video ID.
2. Policy selects candidate clients.
3. Orchestrator requests `/youtubei/v1/player` per candidate.
4. Collect first valid player responses and metadata.
5. Parse formats from streamingData.
6. If needed, resolve player JS and solve `s`/`n` challenges.
7. Emit normalized `VideoInfo` and `FormatInfo`.

## 7. Detailed Phases

### Phase 1: API and Error freeze (package-first)

- Target:
  - `client/client.go`
  - `client/types.go`
  - `client/errors.go`
- Outcome:
  - Public API signatures fixed before internal changes.

### Phase 2: Innertube core

- Target:
  - `internal/innertube/clients.go`
  - `internal/innertube/request.go`
  - `internal/innertube/response.go`
- Outcome:
  - Deterministic request building and response decoding.

### Phase 3: Policy + Orchestrator

- Target:
  - `internal/policy/selector.go`
  - `internal/orchestrator/engine.go`
- Outcome:
  - Predictable fallback and structured failure reporting.

### Phase 4: Format parser

- Target:
  - `internal/formats/parser.go`
- Outcome:
  - Stable metadata extraction independent of stream playback.

### Phase 5: PlayerJS + Challenge

- Target:
  - `internal/playerjs/*`
  - `internal/challenge/*`
- Outcome:
  - URL resolution for ciphered formats.

### Phase 6: Integration to public API

- Target:
  - `client` package wiring
- Outcome:
  - `GetVideo/GetFormats/ResolveStreamURL` functional.

### Phase 7: CLI as adapter (last)

- Target:
  - `cmd/ytv1/main.go`
- Outcome:
  - CLI delegates only to `client` package.
  - No extraction logic allowed in CLI.

## 8. Test Strategy

- Unit tests by package first.
- Orchestrator tests with mocked HTTP responses.
- Fixture-based parser tests for multiple playability shapes.
- One smoke command allowed:
  - `./ytv1.exe -v <id>`
  - but this is verification only, not design target.

## 9. Current Immediate Fixes (before next feature work)

1. `[x]` Make repository build green (`go test ./...`).
2. `[x]` Remove unused imports in `internal/playerjs/*`.
3. `[x]` Ensure selector/client config consistency (API key and fallback behavior).
4. `[x]` Keep binary out of VCS (`ytv1.exe` in `.gitignore`).

## 10. Definition of Done (v1)

v1 is done when:

1. Public package API is stable and documented.
2. `go test ./...` passes reliably.
3. Package can return metadata for known sample IDs.
4. Ciphered formats are either resolvable or return explicit typed errors.
5. CLI remains a thin wrapper over package API.

## 11. Plan Update Rule (Mandatory)

Before starting any substantial change and after finishing it:

1. Update `Current Snapshot` status markers.
2. Move completed items from `Immediate Next Tasks` to done state.
3. Add newly discovered work items with `[ ]`.
4. Keep this file as the single source of truth for execution order.

## 12. Regression Checklist

Run this checklist for every YouTube breakage patch cycle:

1. Confirm `go test ./...` is green before patch.
2. Reproduce breakage with one known sample ID and store exact failing behavior.
3. Verify player URL extraction still returns `/s/player/.../base.js`.
4. Validate `s` decipher path with fixture and one live sample.
5. Validate `n` decipher path for direct URL and manifest URL.
6. Validate fallback behavior for `LOGIN_REQUIRED` and `UNPLAYABLE`.
7. Validate PO token path: provider configured and missing-provider failure cases.
8. Run `go test ./...` after patch and update plan status markers.
