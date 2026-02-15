# ytv1 Implementation Plan (Package-First)

## Status Legend

- `[x]` done
- `[-]` in progress
- `[ ]` pending

## Current Snapshot (Update Every Work Session)

### Immediate Next Tasks (Execution Order)

1. `[x]` Relax PO token requirement handling to non-blocking mode (attempt request without token if provider is missing/fails).
2. `[x]` Add `android_vr` and `web_safari` Innertube profiles and registry aliases.
3. `[x]` Align default selector order to yt-dlp-style priority (`android_vr -> web -> web_safari -> ...`) and update tests.
4. `[x]` Run `go test ./...` and smoke-check `cmd/ytv1` behavior for `jNQXAC9IVRw`.

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
