# ytv1 Detailed Implementation Plan

## 1. Scope and Positioning

- **Project name**: `ytv1`
- **Goal**: A robust, maintainable, and "yt-dlp aligned" YouTube client for Go.
- **Independence**: Do not depend on `kkdai/youtube` runtime behavior; use it only as legacy reference.
- **Core reference**: `yt-dlp` YouTube extractor (logic, constants, and flow).
- **Key differences from legacy**:
  - Context-aware execution (cancellation/timeouts).
  - Multi-client architecture (simulating Android, iOS, Web, TV to evade throttling).
  - Proper PO Token injection support (critical for stability).
  - Structured extraction pipeline (Policy -> Clients -> Orchestrator -> Format Selector).

## 2. Architecture & Responsibilities

### 2.1 Package Structure

- `internal/innertube`:
  - **Registry**: Known clients (`android`, `ios`, `web_creator`, etc.) and their headers/constants.
  - **Request**: Construction of `/player` and `/next` endpoint bodies.
  - **PO Token**: Interfaces for injecting/generating tokens `_base.py:GvsPoTokenPolicy`.
- `internal/policy`:
  - **Selector**: Logic to decide *which* clients to try based on video state (e.g., "Age Gated" -> `tv_embedded` or `web_creator`, "Music" -> `web_music`).
- `internal/playerjs`:
  - **Resolver**: Downloads and caches player JS.
  - **Decipher**: Extracts cipher operations (n-sig, s-sig).
- `internal/formats`:
  - **Processor**: Parses `streamingData` from raw InnerTube responses.
  - **Sorter**: `yt-dlp` style format sorting (Resolution > Bitrate > Codec).
- `internal/orchestrator`:
  - **Engine**: Runs the "Try Client A -> If Fail -> Try Client B" loop.
  - **Merger**: Combines formats found by different clients (e.g. Android gives Playable URL, Web gives 1080p non-playable).
- `pkg/client`:
  - **Client**: High-level public API (`GetVideo`, `GetPlaylist`).
  - **Config**: Configuration options (Proxy, HTTP Client, PO Token Provider).

## 3. Data Model

### 3.1 Core Types

- `VideoInfo`:
  - `ID`, `Title`, `Author`, `Duration`
  - `Formats`: List of `Format`
  - `DASHManifestURL`, `HLSManifestURL`
- `Format`:
  - `Itag`, `URL`, `MimeType`
  - `Bitrate`, `Width`, `Height`, `FPS`
  - `AudioChannels`, `AudioSampleRate`
  - `VideoCodec`, `AudioCodec` (normalized strings)
  - `Protocol`: `https` | `dash` | `hls`
- `Context`:
  - Standard `context.Context` usage for all I/O.

### 3.2 Interfaces

- `PoTokenProvider`:
  - `GetToken(ctx, clientID string) (string, error)`
  - Allows external injection of PO Tokens (e.g. via separate generator or static string).

## 4. Execution Pipeline

1. **Input**: Video ID + specific `Options` (optional).
2. **Policy Selection**: Determine candidate clients.
   - *Default*: `android`, `ios`, `web`, `tv_embedded`.
3. **Execution Loop** (Sequential or Bounded Concurrent):
   - **Step 3a**: Build Request (inject PO Token if available/required).
   - **Step 3b**: Fetch `PlayerResponse`.
   - **Step 3c**: Validate Playability (Status="OK" / "UNPLAYABLE").
4. **Extraction**:
   - Extract raw formats.
5. **Deciphering**:
   - Resolve Player JS (if signature deciphering needed).
   - Solve `n` parameter and `s` signature.
6. **Polishing**:
   - Resolve DASH/HLS manifests if present.
   - Merge formats from all successful clients.
7. **Sorting**:
   - Apply `yt-dlp` sorting qualities (User preference or default Best).
8. **Output**: `VideoInfo` struct.

## 5. Detailed Phases

### Phase 1: Core Abstractions & Configuration
- **Files**: `pkg/client/config.go`, `internal/types/context.go`
- **Tasks**:
  - Define `Config` struct (HTTPClient, Proxy, VisitorData).
  - Define `PoTokenProvider` interface.
  - Define standard `CommonError` types (VideoUnavailable, LoginRequired).

### Phase 2: Innertube Registry & PO Token Support
- **Files**: `internal/innertube/registry.go`, `internal/innertube/clients.go`
- **Tasks**:
  - Port clients from `yt-dlp/_base.py`:
    - `android` (Reliable, no JS required often).
    - `ios` (Good for HLS/Live).
    - `web` (Standard, requires PO Token often).
    - `tv_embedded` (Good for age-gated bypass).
  - Implement `GvsPoTokenPolicy` struct in Go to map requirements.

### Phase 3: Innertube Request/Response
- **Files**: `internal/innertube/request.go`, `internal/innertube/response.go`
- **Tasks**:
  - Implement `PlayerRequest` builder.
  - Support `CheckPlayability` logic.
  - Strict JSON definitions for `StreamingData` and `PlayabilityStatus`.

### Phase 4: Policy & Orchestrator
- **Files**: `internal/policy/selector.go`, `internal/orchestrator/engine.go`
- **Tasks**:
  - Implement default fallback strategy: `Android -> iOS -> Web -> TV`.
  - Handle "Video ID not found" vs "Sign in required" vs "Throttle".

### Phase 5: Player JS & Deciphering
- **Files**: `internal/playerjs/resolver.go`, `internal/playerjs/cache.go`
- **Tasks**:
  - Cache Player JS body by ID/Version.
  - Extract `Decipher` functions using regex (existing `kkdai/youtube` logic acts as reference here).
  - **Crucial**: Implement `n` parameter descrambling (often changes, keep robust).

### Phase 6: Formats & Sorting (The "yt-dlp" equivalent)
- **Files**: `internal/formats/parser.go`, `internal/formats/sort.go`
- **Tasks**:
  - Normalize Codec strings (`avc1.4d401e` -> `h264`).
  - Implement sorting logic:
    - `has_video` & `has_audio` > `resolution` > `fps` > `bitrate`.

### Phase 7: Manifest Handling
- **Files**: `internal/formats/dash.go`, `internal/formats/hls.go`
- **Tasks**:
  - Fetch basic HLS/DASH manifests.
  - *Note*: Full DASH parsing can be complex; start with `GetManifestURL` and basic stream extraction if needed.

### Phase 8: Public API & CLI
- **Files**: `pkg/client/client.go`, `cmd/ytv1/main.go`
- **Tasks**:
  - methods: `GetVideo(ctx, url)`, `GetPlaylist(ctx, url)`.
  - CLI flags: `--proxy`, `--po-token`, `--client-override`.

### Phase 9: Verification & Test Suite
- **Tasks**:
  - **Fixtures**: Capture real raw JSON capabilities from `yt-dlp --dump-json`.
  - **Mock Server**: Test orchestrated fallbacks without hitting real YouTube.
  - **Integration**: "Can we download Video X?" (Daily/Weekly run).

## 6. Immediate Action Plan (Sprint 1)

1.  **Skeleton**: Create package structure `internal/innertube`, `internal/orchestrator`.
2.  **Registry**: Add `android` and `ios` client profiles.
3.  **Request**: Implement basic `Player` endpoint fetcher.
4.  **Integration**: Verify we can get a `200 OK` from YouTube API with a dummy video ID.

## 7. Known Risks

- **PO Token Requirements**: YouTube is aggressively enforcing this.
  - *Mitigation*: We will expose the `PoTokenProvider` interface early so users can plug in generators like `github.com/yt-dlp/yt-dlp-get-pot` or others.
- **Android Client degradation**: Sometimes Android client gets throttled.
  - *Mitigation*: Use `ios` as strong secondary.
