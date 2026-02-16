# Architecture

## Core pipeline

1. Resolve candidate Innertube clients by policy.
2. Fetch player responses and pick usable streamingData.
3. Resolve player JS URL/variant.
4. Collect and solve signature/n challenges.
5. Emit playable stream URLs.

## Module boundaries

- `client/*`: public API surface, lifecycle hooks, user-facing error mapping.
- `internal/orchestrator/*`: client attempt ordering, retries, playability/error aggregation.
- `internal/playerjs/*`: watch-page player path extraction, JS fetch/cache, decipher op extraction.
- `internal/challenge/*`: challenge inventory and solver interfaces.
- `internal/formats/*`: direct + manifest format normalization and expansion.
- `internal/downloader/*`: segmented and retry-aware media transfer internals.

## Event pipeline

Two optional callback channels are exposed through `client.Config`:

1. `OnExtractionEvent`
   - stages: `webpage`, `player_api_json`, `player_js`, `challenge`, `manifest`
   - phases: `start`, `success`, `failure`, `partial`
2. `OnDownloadEvent`
   - stages: `download`, `merge`, `cleanup`
   - phases: destination/start/progress/complete/failure/skip/delete

This keeps diagnostics observable without coupling library internals to CLI output behavior.

## Challenge pipeline

1. Build first-pass inventory of all challenge inputs (`n`, `s`) from direct URLs, cipher URLs, and manifest URLs.
2. Fetch player JS once per player identity and build decipher functions.
3. Batch-solve challenge inputs and write results to an in-memory cache.
4. Materialize final URLs via cache-backed rewrite paths.
5. Emit `challenge` lifecycle events with `success`, `partial`, or `failure` for deterministic diagnostics.

## CLI adapter policy

`cmd/ytv1` remains adapter-only:

- it must consume `client` APIs and hooks only,
- it may format lifecycle events for humans (`--verbose`),
- it must not re-implement extraction/challenge logic.

Cycle B extension of this policy:

- selector parsing/evaluation belongs in `internal/selector` + `client` integration,
- batch control and reporting may be orchestrated in CLI, but media decisions stay in package code,
- archive/idempotency state may be wired by CLI, but skip semantics must remain testable through package boundaries.

## yt-dlp Source Map (Cycle A Baseline)

Primary mapping baseline:

- `D:\yt-dlp\yt_dlp\extractor\youtube\_video.py`
  - `_extract_player_responses` -> `internal/orchestrator/engine.go`, `client/client.go`
  - `_extract_formats_and_subtitles` -> `internal/formats/*`, `client/client.go`
  - challenge collection/solve flow (`n`/`sig`) -> `client/challenge_cache.go`, `internal/playerjs/*`, `internal/challenge/*`
  - PO token fetch/injection flow -> `internal/challenge/*`, `internal/innertube/*`, `client/client.go`
- `D:\yt-dlp\yt_dlp\extractor\youtube\_base.py`
  - Innertube client defaults/context/header generation -> `internal/innertube/*`, `internal/policy/*`
  - visitor/session/cookie-derived request shaping -> `client/request_helpers.go`, `internal/innertube/*`
  - ytcfg/api-key/watch-page data extraction -> `internal/playerjs/*`, `internal/innertube/*`
- `D:\yt-dlp\yt_dlp\extractor\youtube\jsc\*`
  - provider-style JS challenge solving -> `internal/playerjs/*`, `internal/challenge/*`
  - bulk solve and provider fallback semantics -> `client/challenge_cache.go`
- `D:\yt-dlp\yt_dlp\extractor\youtube\pot\*`
  - PO token context/policy/provider/cache abstraction -> `internal/challenge/*`, `internal/innertube/*`
- `D:\yt-dlp\yt_dlp\downloader\http.py`, `fragment.py`, `common.py`
  - HTTP header propagation/range-resume/chunked-retry -> `client/download.go`
  - fragmented transport internals -> `internal/downloader/*`

## Cycle B Gap Focus (CLI Substitute)

Cycle A extraction parity gates are closed. Active gaps are operator workflow parity:

1. Selector grammar depth (`-f`) vs yt-dlp common expressions.
2. Stable output templating and collision policy.
3. Batch controls (`continue`/`abort`) and deterministic summaries.
4. Archive-backed idempotent reruns.
5. Machine-readable diagnostics and explicit exit-code policy.

## Cycle B Regression Matrix Contract

Cycle B validates substitute readiness by workflow class, not single-ID anecdote:

1. Single-item download
2. Selector-heavy download
3. Playlist batch run
4. Subtitle/transcode flow
5. Archive rerun idempotency

Each class must be represented by fixture and/or live-gated tests, with scorecard reporting in `docs/IMPLEMENTATION_PLAN.md`.

## References

- `legacy/kkdai-youtube`
- `d:/yt-dlp/yt_dlp/extractor/youtube`
