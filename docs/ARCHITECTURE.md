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

## yt-dlp Source Map

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

Gap focus (runtime): extraction path reaches playable candidates, but default DSYF download still returns `403`; this indicates remaining parity work in policy/header/token/transport coupling.

## References

- `legacy/kkdai-youtube`
- `d:/yt-dlp/yt_dlp/extractor/youtube`
