# ytv1 Implementation Plan (yt-dlp Deep Port Rebuild)

## Status Legend

- `[x]` done
- `[-]` in progress
- `[ ]` pending
- `[!]` blocked

---

## 0. Planning Rules (Authoritative)

1. This document is the single execution order for migration work.
2. Do not start tasks outside this file unless user explicitly requests.
3. Execute sequentially by dependency track (`R0 -> R11`).
4. Before substantial coding:
   - mark active track as `[-]`
   - keep all later tracks as `[ ]`
5. After substantial coding:
   - mark completed track `[x]`
   - move next track to `[-]` if continuing
   - update `Current Snapshot`
6. Keep package-first architecture; CLI remains adapter-only.
7. Keep public API stable unless a track explicitly declares additive extension.
8. Merge-ready gate per track:
   - `go test ./...` green
   - new behavior covered by tests or explicit TODO with reason
   - no hidden hardcoded runtime behavior without config/fallback

---

## 1. Current Snapshot (Update Every Session)

### 1.1 Session Date
- `2026-02-16`

### 1.2 Verified Runtime State
- `ytv1 DSYFmhjDbvs` now reaches `player_api_json` success and completes JS challenge stage with `challenge:success ... n=1,sig=1` when JS path is used.
- Client selection now resolves by configured order (parallel fetch with deterministic ordered commit), so `android_vr` is preferred when available.
- Latest rerun (`2026-02-16`) confirms full end-to-end success: selected `248+251`, downloaded both streams, merged output, and cleaned intermediates.
- R5 parity increment landed: POT provider now has in-process reuse cache, source-client-aware POT policy evaluation is applied in format filtering, and resolved direct/manifest URLs can receive POT query injection under policy.
- R6 groundwork started: `internal/challenge` now includes provider-backed batch solver primitives, and client challenge priming now executes through bulk solver path instead of per-challenge direct loops.
- R6 parity increment landed: challenge cache key now canonicalizes player locale path (`.../ko_KR/base.js` == `.../en_US/base.js`) to improve cache hit stability, partial solve path emits explicit warning logs, and provider fallback chain is enabled for bulk solve resilience.
- R7 parity increment landed: normalized format model now marks `is_drm` and `is_damaged` candidates, and client-side format filtering deterministically skips these before selection/materialization.
- R7 parity increment landed: format ranking now deprioritizes `unknown` protocol candidates versus known media protocols (`https`/`dash`/`hls`) to reduce invalid materialization picks.
- R8 parity increment landed: downloader manifest/segment requests now support explicit media-header propagation for HLS/DASH paths (including default UA/Origin/Referer synthesis) with dedicated transport tests.
- R8 parity increment landed: fragmented downloader paths now honor retry/backoff policy with shared HTTP retry transport (including `Retry-After` throttle hint handling for status retries such as `429`).
- R8 parity increment landed: live/dynamic fragment downloads now support controlled unavailable-fragment skipping (`404`/`410`) with bounded skip limits to mirror practical degraded-stream behavior without hanging on missing edges.
- R8 parity increment landed: DASH static fragment path now supports bounded parallel fetch with ordered materialization (concurrency controlled by transport config), while dynamic/live paths retain sequential semantics.
- R10 parity increment landed: CLI now supports explicit static POT override input via `--po-token` (wired to `client.Config.PoTokenProvider`), and runtime diagnostics emit actionable remediation hints for policy-sensitive failures.
- R11 closeout verification (`2026-02-16`) passed: `go test ./...` green, live-gated E2E suite green, and runtime gate `go run ./cmd/ytv1 --verbose DSYFmhjDbvs` succeeded with download+merge completion.
- Remaining migration work is parity hardening/documentation, not DSYF baseline pass gate failure.

### 1.3 Reference Baseline Used For Rebuild
- Runtime binary folder exists: `C:\yt-dlp`.
- Source reference used for porting: `D:\yt-dlp\yt_dlp`.
- Primary source files for this rebuild:
  1. `D:\yt-dlp\yt_dlp\extractor\youtube\_base.py`
  2. `D:\yt-dlp\yt_dlp\extractor\youtube\_video.py`
  3. `D:\yt-dlp\yt_dlp\extractor\youtube\jsc\*`
  4. `D:\yt-dlp\yt_dlp\extractor\youtube\pot\*`
  5. `D:\yt-dlp\yt_dlp\downloader\http.py`
  6. `D:\yt-dlp\yt_dlp\downloader\fragment.py`
  7. `D:\yt-dlp\yt_dlp\downloader\common.py`

### 1.4 Immediate Next Tasks (Strict Order)
1. `[x]` R0. Rebaseline and failure observability hardening
2. `[x]` R1. yt-dlp YouTube source-map parity (function-level)
3. `[x]` R2. Innertube client/profile parity port
4. `[x]` R3. API header/session/auth parity port
5. `[x]` R4. Player response pipeline parity (`ytcfg`, player URL, `sts`, context)
6. `[x]` R5. PO Token framework parity (policy + provider/cache + URL injection)
7. `[x]` R6. JS challenge framework parity (bulk solve + provider fallback + cache semantics)
8. `[x]` R7. Format/materialization parity (direct + manifest + n/sig/pot rewrite)
9. `[x]` R8. Downloader transport parity (HTTP/fragment retry, range/chunk/resume, header propagation)
10. `[x]` R9. End-to-end regression matrix (DSYF mandatory pass gate)
11. `[x]` R10. CLI parity diagnostics and operator controls
12. `[x]` R11. Migration closeout with unresolved-gap classification

---

## 2. Mission and Non-Negotiable Outcome

### 2.1 Mission
`ytv1` must port yt-dlp YouTube extraction behavior deeply enough that real-world IDs like `DSYFmhjDbvs` succeed in default usage, not only fixture tests.

### 2.2 Non-Negotiable Outcome
- `ytv1 DSYFmhjDbvs` succeeds end-to-end on maintained environment.
- If blocked by external factors, diagnostics must explicitly identify exact blocker (policy/auth/pot/challenge/download).

---

## 3. Execution Tracks

### R0. Rebaseline and Failure Observability
- Status: `[x]`
- Goal: Freeze current failure signatures before deeper porting.
- Work:
  1. Capture structured stage logs for extraction + download attempts for DSYF.
  2. Ensure diagnostics include selected client, protocol, URL policy metadata, and response status.
  3. Add regression test harness hooks for replaying critical failure surfaces.
- Target files:
  - `client/client.go`
  - `client/download.go`
  - `client/errors.go`
  - `cmd/ytv1/main.go`
- Acceptance:
  - DSYF failure path produces deterministic, typed diagnostics suitable for root-cause triage.

### R1. yt-dlp YouTube Source-Map Parity
- Status: `[x]`
- Goal: Build explicit mapping from yt-dlp functions to ytv1 modules before additional code movement.
- Work:
  1. Map `_video.py` extraction flow to `client` + `internal/orchestrator`.
  2. Map `_base.py` header/session/context helpers to `internal/innertube` + request helpers.
  3. Map `jsc/*` and `pot/*` responsibilities to `internal/playerjs` + `internal/challenge`.
  4. Map downloader `http.py`/`fragment.py` behavior to `client/download` + `internal/downloader`.
- Target files:
  - `docs/IMPLEMENTATION_PLAN.md`
  - `docs/ARCHITECTURE.md`
- Acceptance:
  - No major yt-dlp dependency path is left unmapped.

### R2. Innertube Client/Profile Parity Port
- Status: `[x]`
- Goal: Align client matrix and fallback behavior with yt-dlp practical defaults.
- Work:
  1. Port effective default client order and auth/non-auth variants.
  2. Port per-client capability metadata (JS required, cookie support, ad playback context support).
  3. Port client-specific host/context/version invariants needed for successful playback URL extraction.
- Target files:
  - `internal/innertube/clients.go`
  - `internal/policy/*`
  - `internal/orchestrator/*`
- Acceptance:
  - Client trial order and fallback decisions are deterministic and policy-driven.

### R3. API Header/Session/Auth Parity Port
- Status: `[x]`
- Goal: Match yt-dlp request identity behavior used for YouTube API and playback eligibility.
- Work:
  1. Port visitor data extraction/override precedence.
  2. Port API headers generation (`X-YouTube-Client-*`, visitor, UA, auth/session headers).
  3. Port cookie/session-derived auth header behavior (where applicable without login automation).
- Target files:
  - `internal/innertube/*`
  - `client/request_helpers.go`
  - `client/config.go`
- Acceptance:
  - Header/session behavior is explicit, testable, and client-aware.

### R4. Player Response Pipeline Parity
- Status: `[x]`
- Goal: Align player response fetching with yt-dlp’s multi-source/context usage.
- Work:
  1. Port ytcfg acquisition per client and fallback strategy.
  2. Port player URL extraction precedence and robust fallback.
  3. Port signature timestamp (`sts`) usage in player context.
  4. Port player-response request context shaping per client.
- Target files:
  - `internal/orchestrator/engine.go`
  - `internal/playerjs/*`
  - `client/client.go`
- Acceptance:
  - Player response fetching matches yt-dlp decision points for core clients.

### R5. PO Token Framework Parity
- Status: `[x]`
- Goal: Implement policy-correct PO Token handling for player/GVS/subs paths.
- Work:
  1. Port token policy semantics (required/recommended/never) by protocol and client.
  2. Port fetch lifecycle and caching behavior for token reuse.
  3. Port URL/query/path injection for formats/manifests/subtitles.
  4. Port typed warnings when formats are skipped due to missing required POT.
- Target files:
  - `internal/challenge/*`
  - `internal/innertube/*`
  - `client/client.go`
  - `client/stream_api.go`
- Acceptance:
  - Missing/available POT behavior is deterministic and visible in diagnostics.

### R6. JS Challenge Framework Parity
- Status: `[x]`
- Goal: Align challenge solve behavior with yt-dlp’s batch/provider model.
- Work:
  1. Port first-pass challenge inventory across direct/cipher/manifest URLs.
  2. Port provider-style bulk solve semantics and fallback handling.
  3. Port cache key semantics for `n` and `sig` outputs tied to player identity.
  4. Port partial-failure degradation behavior and warnings.
- Target files:
  - `internal/playerjs/*`
  - `internal/challenge/*`
  - `client/challenge_cache.go`
- Acceptance:
  - Challenge solving is bulk-first, cached, and failure-mode compatible.

### R7. Format/Materialization Parity
- Status: `[x]`
- Goal: Align final URL materialization and format filtering decisions.
- Work:
  1. Port direct and signatureCipher URL handling details.
  2. Port n/sig rewrite ordering and query normalization.
  3. Port manifest n rewrite and POT path/query injection.
  4. Port skip/deprioritize policies (damaged/DRM/missing POT).
- Target files:
  - `internal/formats/*`
  - `client/client.go`
  - `client/stream_api.go`
- Acceptance:
  - Selected formats translate into valid downloadable URLs under real conditions.

### R8. Downloader Transport Parity
- Status: `[x]`
- Goal: Match yt-dlp download transport behavior needed to avoid 403 and transient failures.
- Work:
  1. Port per-request media header propagation strategy.
  2. Port range/chunk/resume logic parity for HTTP downloads.
  3. Port retry/throttle behavior for direct and fragmented transfers.
  4. Port fragment concurrency/error-skip policies where applicable.
- Target files:
  - `client/download.go`
  - `internal/downloader/*`
  - `client/request_helpers.go`
- Acceptance:
  - DSYF selected format download no longer fails with immediate 403 under baseline environment.

### R9. End-to-End Regression Matrix
- Status: `[x]`
- Goal: Replace synthetic confidence with real parity confidence.
- Work:
  1. Keep fixture tests for deterministic unit behavior.
  2. Add controlled integration tests mirroring current breakpoints.
  3. Add manual/CI verification checklist including mandatory DSYF pass.
- Target files:
  - `client/*_test.go`
  - `internal/*/*_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Regression suite catches known extraction/download parity regressions.
  - Verification checklist:
    1. `go test ./...` is green in default mode.
    2. `YTV1_E2E=1 go test ./client -run TestE2E_ -count=1` passes against live endpoint.
    3. DSYF mandatory gate passes in runtime: `go run ./cmd/ytv1 --verbose DSYFmhjDbvs`.

### R10. CLI Diagnostics and Operator Controls
- Status: `[x]`
- Goal: Make parity gaps operable without moving logic into CLI.
- Work:
  1. Expose extraction/download diagnostics clearly in verbose mode.
  2. Add explicit flags for visitor data, client override, POT override inputs.
  3. Ensure diagnostics include actionable remediation hints.
- Target files:
  - `cmd/ytv1/main.go`
  - `internal/cli/parser.go`
  - `README.md`
- Acceptance:
  - User can identify and adjust policy-sensitive failures from CLI output alone.

### R11. Migration Closeout
- Status: `[x]`
- Goal: Finalize with explicit pass/fail truth.
- Work:
  1. Run full test sweep and targeted runtime validation.
  2. Mark completed/blocked tracks with evidence.
  3. Record unresolved items with concrete blocker reasons.
- Target files:
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Final plan state reflects runtime reality, not just unit pass status.

---

## 4. Public API Contract

1. Preserve `client.New`, `GetVideo`, `GetFormats`, `ResolveStreamURL` behavior.
2. Prefer additive config/events over breaking signatures.
3. Maintain `errors.Is` compatibility for sentinel errors.

---

## 5. Done Criteria (Global)

Global migration is considered complete only when all are true:
1. `R0-R11` are `[x]` or explicitly `[!]` with reason.
2. `go test ./...` is green.
3. `ytv1 DSYFmhjDbvs` succeeds end-to-end in baseline run.
4. Remaining gaps are documented with exact blocker classes.

---

## 6. Change Log (Plan)

- `2026-02-16`: Rebuilt plan from scratch based on deep source review of `D:\yt-dlp\yt_dlp\extractor\youtube/*`, `jsc/*`, `pot/*`, and downloader transport modules, with DSYF 403 runtime failure as primary migration gate.
- `2026-02-16`: Completed R0 (download failure typed diagnostics, source-client propagation, protocol/host/url-policy metadata in runtime and CLI diagnostics); moved R1 to in-progress.
- `2026-02-16`: Completed R1 source-map parity documentation (`docs/ARCHITECTURE.md`) with yt-dlp function-to-module mapping across extractor/jsc/pot/downloader paths.
- `2026-02-16`: Started R2 implementation: added client alias ID propagation (`web` vs `web_safari`) for deterministic diagnostics, aligned default selector order to yt-dlp baseline (`android_vr`, `web`, `web_safari`) with authenticated variant (`tv_downgraded`, `web`, `web_safari`), and added client capability metadata fields.
- `2026-02-16`: Added initial R3 header parity subset in Innertube requests (`X-YouTube-Client-Name`, `X-YouTube-Client-Version`, `X-Goog-Visitor-Id`, `X-Origin`) with orchestrator tests.
- `2026-02-16`: Completed R2 (client profile/order parity baseline) and moved R3 to in-progress.
- `2026-02-16`: Extended R3 with cookie-derived auth/session signal handling (`Authorization` SAPISID hash variants, `X-Youtube-Bootstrap-Logged-In`) and visitor-data fallback from cookie jar (`VISITOR_INFO1_LIVE`), with unit/integration tests.
- `2026-02-16`: Extended R3 visitor precedence with watch-page `ytcfg` fallback (`VISITOR_DATA`) via `APIKeyResolver` cache path, including resolver/orchestrator tests.
- `2026-02-16`: Extended R3 session header parity with watch-page metadata extraction (`SESSION_INDEX`, `DELEGATED_SESSION_ID`, `USER_SESSION_ID`, `DATASYNC_ID`) and request header propagation (`X-Goog-AuthUser`, `X-Goog-PageId`); aligned SID hash format with yt-dlp additional `u` marker semantics.
- `2026-02-16`: Started R4 groundwork by parsing watch-page `STS` and injecting `playbackContext.contentPlaybackContext.signatureTimestamp` into Innertube player requests; added resolver/request/orchestrator coverage.
- `2026-02-16`: Extended R4 player URL extraction precedence in `internal/playerjs` to prefer watch-page `PLAYER_JS_URL` and `WEB_PLAYER_CONTEXT_CONFIGS.*.jsUrl` before generic regex fallback, with dedicated tests.
- `2026-02-16`: Completed R3 (client-aware header/session/auth parity baseline) and moved R4 to in-progress.
- `2026-02-16`: R4 ytcfg acquisition path now uses client-aware page selection and per-client cache keying (`host|client`) instead of fixed watch seed; DSYF runtime advanced to challenge stage and now fails with `challenge not solved`.
- `2026-02-16`: Extended R4 request/context parity with optional ad playback context and top-level player params plumbing; added STS fallback extraction from player JS when watch-page `STS` is absent, and updated tests/mocks for player-JS fetch path (`go test ./...` green).
- `2026-02-16`: Added runtime JS decipher fallback in `internal/playerjs/decipher.go` (export-injected goja execution path) when regex token extraction fails; `challenge` stage for DSYF now reports success (`n=1,sig=1`) and remaining runtime blocker moved to download-side `403` (POT/SABR/transport parity).
- `2026-02-16`: Updated orchestrator client selection to deterministic order-priority commit while keeping concurrent fetches; DSYF baseline now selects `android_vr` first and `ytv1 DSYFmhjDbvs` succeeds end-to-end (`248+251` download + merge).
- `2026-02-16`: Completed R5 baseline parity increment: added cached POT provider wrapper (`internal/challenge`), source-client-aware POT policy resolution in client format filtering, POT URL injection for direct/manifest materialization paths, and warning visibility for POT-required skip reasons (`go test ./...` green).
- `2026-02-16`: Started R6 implementation by adding provider-style batch challenge solver abstractions/tests in `internal/challenge` and wiring client challenge priming to bulk solve semantics (`go test ./...` green).
- `2026-02-16`: Extended R6 cache semantics by canonicalizing challenge cache keys per player identity (locale-insensitive base.js path) and adding explicit partial-solve warning log surface (`go test ./...` green).
- `2026-02-16`: Completed R6 by adding fallback provider-chain bulk solver semantics and wiring client challenge priming to try canonicalized provider fallback path when primary solve path fails (`go test ./...` green); moved R7 to in-progress.
- `2026-02-16`: Extended R7 with format-quality guardrails: parsed formats now carry `is_drm`/`is_damaged` signals (including cipher-url integrity check), and `filterFormatsByPoTokenPolicy` now drops DRM/damaged candidates with typed skip reasons (`go test ./...` green).
- `2026-02-16`: Extended R7 ranking policy by adding protocol-awareness in selector tie-breaks so `unknown` protocol formats are deprioritized against known protocols under equivalent mode/quality classes (`go test ./...` green).
- `2026-02-16`: Completed R7 after porting direct/cipher URL materialization, n/sig rewrite flow, manifest n+POT handling, and skip/deprioritize policy hardening (`go test ./...` green); moved R8 to in-progress.
- `2026-02-16`: Started R8 transport parity increment by wiring request-header propagation into HLS/DASH downloader fetch paths and adding coverage for manifest+segment header forwarding (`go test ./...` green).
- `2026-02-16`: Extended R8 transport retry parity by introducing shared downloader retry transport config for HLS/DASH (manifest/segment/key paths), mapping package transport config into fragmented downloads, and adding retries + `Retry-After` coverage tests (`go test ./...` green).
- `2026-02-16`: Hardened R8 fragmented transport control by propagating caller context into HLS key-fetch path (replacing background context) so cancellation/deadline semantics remain consistent during live/fragmented retries (`go test ./...` green).
- `2026-02-16`: Extended R8 fragment policy parity by adding optional unavailable-fragment skip semantics for HLS live and DASH dynamic flows (bounded by max-skip guard) and wiring new transport knobs through package config (`go test ./...` green).
- `2026-02-16`: Extended R8 concurrency parity by plumbing transport max-concurrency into fragmented downloader config and enabling ordered parallel segment fetch for DASH static manifests, with dedicated ordering coverage (`go test ./...` green).
- `2026-02-16`: Completed R8 with baseline runtime verification (`go run ./cmd/ytv1 --verbose -o r8_dsyf_check.mp4 DSYFmhjDbvs`) confirming DSYF extraction/download/merge success without immediate `403`; moved R9 to in-progress.
- `2026-02-16`: Started R9 by adding controlled live integration smoke coverage (`client/e2e_integration_test.go`) gated behind `YTV1_E2E=1` (default CI-safe skip) for DSYF/override video download regression checks.
- `2026-02-16`: Completed R9 by extending controlled live integration coverage (`GetVideo/GetFormats`, `ResolveStreamURL`, `Download`) and validating both default suite (`go test ./...`) and live-gated suite (`YTV1_E2E=1 go test ./client -run TestE2E_ -count=1`); moved R10 to in-progress.
- `2026-02-16`: Completed R10 by adding CLI POT override input (`--po-token` -> static provider wiring), enabling diagnostics emission in verbose failure paths, adding actionable remediation hints (login/POT/challenge/throttle/input), updating CLI README guidance, and validating with `go test ./...`; moved R11 to in-progress.
- `2026-02-16`: Completed R11 closeout verification: full suite `go test ./...` passed, live-gated E2E (`YTV1_E2E=1 go test ./client -run TestE2E_ -count=1 -timeout 8m`) passed, and runtime DSYF gate (`go run ./cmd/ytv1 --verbose DSYFmhjDbvs`) passed end-to-end (extract/download/merge); all tracks now complete.
