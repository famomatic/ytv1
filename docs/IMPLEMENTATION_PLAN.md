# ytv1 Implementation Plan (Package-First, Detailed)

## Status Legend

- `[x]` done
- `[-]` in progress
- `[ ]` pending
- `[!]` blocked

---

## 0. Planning Rules (Authoritative)

1. This document is the single source of truth for execution order.
2. Do not start a task not listed here unless explicitly requested by user.
3. Before substantial coding:
   - mark current task as `[-]`
   - ensure all prior tasks in order are `[x]` or `[!]`
4. After substantial coding:
   - mark completed tasks `[x]`
   - update `Current Snapshot`
   - append newly discovered tasks under `Immediate Next Tasks`
5. Keep package API stable unless a dedicated task here explicitly allows breaking changes.
6. CLI must remain adapter-only; extraction logic belongs in `client`/`internal`.
7. Each task is merge-ready only if:
   - `go test ./...` passes
   - behavior is covered by tests or TODO with explicit reason
   - no hidden hardcoded runtime behavior without config override

---

## 1. Current Snapshot (Update Every Session)

### 1.1 Session Date
- `2026-02-16`

### 1.2 Verified Baseline
- `go test ./...` is green at session start.
- Existing major phases are marked done in previous plan, but behavior-gap audit identified remaining functional/quality debt.

### 1.3 Immediate Next Tasks (Strict Execution Order)

1. `[x]` Rebuild plan and gap-driven execution queue (this task)
2. `[x]` Fix direct URL `n` decipher bypass in `ResolveStreamURL`
3. `[x]` Refine format protocol normalization to avoid false `https` fallback for manifest/cipher cases
4. `[x]` Implement manifest expansion layer (DASH/HLS parse -> normalized `FormatInfo` candidates)
5. `[x]` Add typed geo/DRM/playability detail model and map from Innertube responses
6. `[x]` Strengthen PO token decision matrix (client+protocol+required/recommended context)
7. `[x]` Remove package-side stdout logging and route through optional logger interface
8. `[x]` Activate `ProxyURL` end-to-end or remove field (choose one and document)
9. `[x]` Add bounded session cache strategy (TTL/LRU + config knobs)
10. `[x]` Expand public metadata surface (`Description`, `DurationSec`, `ViewCount`, etc.)
11. `[x]` Normalize playlist item duration type and semantics
12. `[x]` Add stream-first package API (`OpenStream`/`OpenFormatStream`) for non-file consumers
13. `[x]` Expand subtitles/transcript capability (track preference, fallback policy, richer metadata)
14. `[x]` Add regression fixtures/tests for new branches (n-decipher, manifest parse, playability matrix)
15. `[x]` Update README/API docs/examples for all changed behavior
16. `[x]` Playlist continuation robustness v2 with typed diagnostics and loop guards

### 1.4 Known Risk Flags

- Remaining debt is primarily deeper yt-dlp parity work (auth/cookies/live-chat/live edge cases).

---

## 2. Mission and Scope

## 2.1 Mission

`ytv1` is a Go-native YouTube extraction library with package API as product.

## 2.2 Functional Scope (v1)

Public API (must remain stable unless scheduled task says otherwise):

1. `client.New(config)`
2. `client.GetVideo(ctx, input)`
3. `client.GetFormats(ctx, input)`
4. `client.ResolveStreamURL(ctx, videoID, itag)`

Additional package APIs currently in scope:

1. `GetPlaylist`
2. `GetTranscript`
3. Subtitle track listing/fetch
4. Download and transcode pathways

## 2.3 Out of Scope (until explicitly added)

- Full yt-dlp feature parity across every extractor flag.
- Account-login emulation/cookie automation beyond documented config surface.
- CLI-first feature additions.

---

## 3. References and Porting Policy

Behavior references:

1. `legacy/kkdai-youtube`
2. `D:/yt-dlp/yt_dlp/extractor/youtube`

Porting rule:

1. Port behavior, not source structure.
2. Keep implementation idiomatic Go.
3. Avoid hard dependency on Python runtime.

---

## 4. Architecture Contract

## 4.1 Public Layer

- `client/*`: API, config, errors, orchestration wiring

## 4.2 Internal Layers

- `internal/innertube`: request/response/client profiles
- `internal/policy`: client selection order/skip/override
- `internal/orchestrator`: request race/fallback/retry/error mapping
- `internal/formats`: normalization + manifest expansion
- `internal/playerjs`: player URL/js fetch/cache + decipher
- `internal/challenge`: challenge solving interfaces
- `internal/stream`: stream/challenge processing abstractions
- `internal/httpx`: HTTP abstractions

## 4.3 Layering Rules

1. `cmd/ytv1` must not contain extraction decisions.
2. `client` may depend on `internal/*`; reverse is forbidden.
3. `internal/*` modules should expose deterministic behavior with fixture tests.

---

## 5. Detailed Work Plan

## 5.1 Track A: Correctness and Extraction Fidelity

### A1. ResolveStreamURL direct URL n-decipher correctness
- Status: `[ ]`
- Goal:
  - Ensure direct URL path also applies `n` decipher when present.
- Files:
  - `client/client.go`
  - tests in `client/resolve_stream_url_test.go`
- Acceptance:
  - direct URL with `n` query is rewritten using playerjs decipher
  - no regression for ciphered URL flows

### A2. Protocol normalization fidelity
- Status: `[ ]`
- Goal:
  - Avoid over-classifying unknown/ciphered/manifest-derived formats as `https`.
- Files:
  - `internal/formats/parser.go`
  - `internal/formats/parser_test.go`
- Acceptance:
  - protocol reflects source reliably (`https`/`dash`/`hls`/`unknown` as needed)
  - PO token filter decisions use correct protocol

### A3. Manifest expansion pipeline
- Status: `[ ]`
- Goal:
  - Parse DASH/HLS manifests into normalized candidate formats, not raw string only.
- Files:
  - `internal/formats/dash.go`
  - `internal/formats/hls.go`
  - `client/client.go` integration
  - tests: new fixture-driven parser tests
- Acceptance:
  - manifest URLs yield additional format candidates
  - errors are typed and non-fatal when fallback candidates exist

### A4. Playability detail taxonomy (geo/drm/login/unavailable)
- Status: `[x]`
- Goal:
  - Replace string-only heuristics with structured detail fields while preserving sentinel compatibility.
- Files:
  - `internal/orchestrator/errors.go`
  - `client/errors.go`
  - `client/client.go`
- Acceptance:
  - `errors.Is` compatibility retained
  - `errors.As` provides typed detail including geo/drm dimensions when available

---

## 5.2 Track B: Policy and Access Control

### B1. PO token matrix hardening
- Status: `[x]`
- Goal:
  - Support required/recommended behavior with protocol-specific enforcement and diagnostics.
- Files:
  - `internal/orchestrator/engine.go`
  - `client/pot_filter.go`
  - tests in `client/pot_filter_test.go` + orchestrator tests
- Acceptance:
  - required policy drops/blocks appropriately
  - recommended policy preserves formats with warning detail
  - diagnostics include reason and protocol

### B2. ProxyURL effective behavior
- Status: `[x]`
- Decision task:
  - Option 1: wire `ProxyURL` into transport builder
  - Option 2: remove/deprecate `ProxyURL` and rely on provided `HTTPClient`
- Files:
  - `client/config.go`
  - `internal/innertube/config.go`
  - possibly transport helper additions
- Acceptance:
  - chosen behavior is real, tested, and documented

---

## 5.3 Track C: Package Product Quality

### C1. Remove package stdout side effects
- Status: `[x]`
- Goal:
  - Eliminate direct `fmt.Printf` from package logic.
- Files:
  - `client/playlist_transcript.go`
  - config/logger surface if needed
- Acceptance:
  - library emits no uncontrolled stdout/stderr
  - warnings exposed via error detail or optional logger

### C2. Session cache bounds
- Status: `[x]`
- Goal:
  - Prevent unbounded growth of `sessions` map in long-lived processes.
- Files:
  - `client/client.go`
  - `client/config.go`
  - tests for eviction/TTL behavior
- Acceptance:
  - configurable bound or TTL implemented
  - concurrent access remains race-safe

### C3. Expand public metadata contract
- Status: `[x]`
- Goal:
  - Add key fields expected from package consumers (Description, DurationSec, ViewCount, etc.).
- Files:
  - `client/types.go`
  - `client/client.go`
  - tests in `client/getvideo_getformats_test.go`
- Acceptance:
  - fields populated when available
  - backward compatibility maintained for existing fields

### C4. Playlist item duration normalization
- Status: `[x]`
- Goal:
  - Replace ambiguous string-only duration with structured representation.
- Files:
  - `client/types.go`
  - `client/playlist_transcript.go`
  - tests
- Acceptance:
  - machine-usable duration field added
  - display text can remain separate optional field

### C5. Stream-first API for package consumers
- Status: `[x]`
- Goal:
  - Add reader-based API to avoid mandatory file download path.
- Files:
  - `client` package (new API + tests)
- Acceptance:
  - callers can resolve and consume stream via `io.ReadCloser`
  - context cancellation and retry behavior documented

---

## 5.4 Track D: Subtitles/Transcript/Playlist Depth

### D1. Subtitle track preference policy
- Status: `[x]`
- Goal:
  - Add deterministic track selection with options (exact language, fallback, auto-gen preference).
- Files:
  - `client/playlist_transcript.go`
  - `client/config.go` (if policy exposed globally)
- Acceptance:
  - no-language default is explicit policy, not first-track accident

### D2. Transcript robustness
- Status: `[x]`
- Goal:
  - Improve parsing resilience and error typing for unavailable/disabled/malformed responses.
- Files:
  - `client/playlist_transcript.go`
  - tests
- Acceptance:
  - caller can distinguish unavailable vs parsing failure

### D3. Playlist continuation robustness v2
- Status: `[x]`
- Goal:
  - Keep resilient token traversal and prevent duplicates/loops with typed diagnostics.
- Files:
  - `client/playlist_transcript.go`
  - tests
- Acceptance:
  - stable traversal across mixed valid/invalid continuation tokens

---

## 5.5 Track E: Testing and Regression Safety

### E1. Fixture additions
- Status: `[x]`
- Add fixtures for:
  - direct URL requiring `n` rewrite
  - manifest-only videos (DASH/HLS)
  - geo/login/drm playability variants
  - subtitles track edge cases

### E2. Integration-like behavior tests
- Status: `[x]`
- Add tests for:
  - fallback client behavior under override modes
  - PO token required/recommended matrix
  - session cache eviction behavior

### E3. Non-functional checks
- Status: `[x]`
- Ensure:
  - `go test ./...` green
  - race-risk areas covered by concurrency-sensitive tests where practical

---

## 6. Public API and Error Contract (v1 Snapshot)

## 6.1 Stable APIs

- `New`, `GetVideo`, `GetFormats`, `ResolveStreamURL`
- existing playlist/transcript/subtitle APIs remain supported

## 6.2 Error Compatibility Rules

1. Sentinel compatibility via `errors.Is` must be preserved.
2. Rich diagnostics via typed detail errors via `errors.As`.
3. New typed errors must include migration-safe fallback to existing sentinels.

---

## 7. Definition of Done (Per Task)

A task is complete only when all are true:

1. Code implemented.
2. Tests added/updated.
3. `go test ./...` passes.
4. Plan markers updated.
5. README/docs updated if public behavior changed.

---

## 8. Session Checklist (Mandatory)

Before work:
1. Review `Current Snapshot`.
2. Mark target task `[-]`.
3. Confirm no earlier ordered task remains unresolved.

After work:
1. Mark completed task `[x]`.
2. Move next task to `[-]` if continuing.
3. Record newly discovered tasks in `Immediate Next Tasks`.
4. Re-run `go test ./...`.

---

## 9. Immediate Backlog Notes (From Gap Audit)

- yt-dlp parity still missing in:
  - cookie-auth header generation flows
  - live/post-live specific format handling
  - richer subtitle translation/live-chat extraction
- kkdai parity still missing in package ergonomics:
  - richer metadata model
  - stream-returning APIs for non-file consumers
- Quality debt:
  - stdout logging in package code
  - unused/dead config fields (`ProxyURL` path validation required)

---

## 10. Change Log (Plan)

- `2026-02-16`: Full plan rewrite from coarse “mostly done” checklist to gap-driven, ordered, testable execution plan.
- `2026-02-16`: Completed Task 13~15 (subtitle policy + transcript typed errors + regression coverage + README/API refresh).
- `2026-02-16`: Completed Task 16 + continuation diagnostics and loop guards; added concurrent session-cache access test.
