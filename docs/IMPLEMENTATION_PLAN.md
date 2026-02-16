# ytv1 Implementation Plan (Cycle B: YouTube CLI Substitute)

## Status Legend

- `[x]` done
- `[-]` in progress
- `[ ]` pending
- `[!]` blocked

---

## 0. Planning Rules (Authoritative)

1. This document is the single execution order for active work.
2. Do not start tasks outside this file unless user explicitly requests.
3. Execute sequentially by dependency track (`B0 -> B9`).
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

### 1.2 Completed Baseline (Cycle A Closed)
- Previous migration cycle `R0-R11` is fully completed.
- DSYF runtime gate already passed end-to-end (`extract + download + merge`).
- Live-gated E2E tests and default test suite were green at cycle closeout.
- Core parity blocks are in place: Innertube policy, JS challenge, POT policy/injection, format materialization, retryable downloader transport, CLI diagnostics.

### 1.3 Current Gap Theme (New Cycle Focus)
- CLI still supports only a narrow selector mapping (`best`, `bestaudio`, `bestvideo`, `mp4`, `mp3`) and lacks yt-dlp-grade selection grammar behavior.
- CLI/operator workflow parity is incomplete (batch controls, archive/idempotency, richer output templating, predictable failure controls).
- Need explicit substitute-level acceptance criteria and regression matrix focused on real operator workflows, not only single-ID success.
- B0 documentation sync is complete across plan/readme/architecture with explicit scorecard targets and workflow matrix classes.
- B1 first increment landed: CLI now forwards custom `-f` expressions to package selector flow, while preserving mp3/mode aliases and numeric itag input handling.
- B1 second increment landed in selector engine: fixed `ext=m4a` matching for `audio/mp4`, added width-filter semantics, improved `best` ranking to prefer AV candidates, and added selector behavior tests for merge/fallback/filter paths.
- B1 third increment landed: added `worstvideo/worstaudio` selection semantics and standalone predicate parsing (`fps!=60`, `ext=mp4`, `height<=720`) for fallback segments.
- B1 completion increment landed: selector parse/match failures now surface as typed `ErrNoPlayableFormats` details with selector+reason diagnostics, and CLI remediation hints include selector-specific guidance.
- B2 completion increment landed: output template token rendering (`%(id)s`, `%(title)s`, `%(uploader)s`, `%(ext)s`, `%(itag)s`) now applies deterministically to single/merge downloads with filename-safe normalization and test coverage.
- B3 first increment landed: CLI now exposes batch error policy (`--abort-on-error`), resume policy (`--no-continue`), and retry controls (`--retries`, `--retry-sleep-ms`) mapped into package transport config; main-loop abort semantics are covered by command-layer tests.
- B3 completion increment landed: resume control is wired through CLI-to-download pipeline (`Resume` propagation), retry/backoff overrides are test-covered at config mapping layer, and batch continue/abort semantics are validated with deterministic processor-loop tests.
- B4 completion increment landed: playlist item processing now reports deterministic per-item outcomes with final totals (`total/succeeded/failed/aborted`) and explicit abort-on-error cutover semantics validated by command-layer tests.
- B5 completion increment landed: subtitle flags now execute end-to-end in CLI (`--write-subs`, `--write-auto-subs`, `--sub-lang`) with deterministic SRT naming/output, explicit language fallback policy wiring, and mp3-mode failure remediation hints.
- B6 completion increment landed: CLI archive-backed idempotency is now available via `--download-archive`, with deterministic skip-on-rerun policy, append-on-success persistence, and corruption-tolerant archive load behavior.
- B7 completion increment landed: CLI now emits structured JSON failure payloads in `--print-json` mode (`ok=false`, `input`, `exit_code`, `error{category,message,attempts?}`), exposes explicit exit-code-by-category policy, and uses shared package error categorization for automation-stable diagnostics.
- B8 completion increment landed: fixture workflow matrix is now codified by command-layer matrix tests (`TestWorkflowMatrix_FixtureCoverage`) with reproducible command contract (`go test ./...`, live-gated `YTV1_E2E=1 go test ./client -run TestE2E_`), and latest fixture-gated scorecard on `2026-02-16` is `workflow_pass_rate=100%`, `deterministic_output_rate=100%`, `diagnosable_failure_rate=100%` (inferred from passing matrix-class tests).
- B9 closeout verification landed: `go test ./...` and live-gated matrix (`YTV1_E2E=1 go test ./client -run TestE2E_ -count=1 -timeout 8m`) passed on `2026-02-16`, and residual risk register is now explicit with severity/owner.
- Post-closeout CLI compatibility increment landed: added yt-dlp-style short JSON flag alias (`-J`) mapped to existing `--print-json` behavior with parser test coverage.
- Post-closeout CLI compatibility gap remains for common yt-dlp operator aliases (for example `--flat-playlist` and related playlist/error-handling flags).
- B10 completion increment landed: added yt-dlp compatibility aliases (`--flat-playlist`/`--extract-flat`, `--no-playlist`/`--yes-playlist`, `--ignore-errors`/`-i`, `-j`/`--dump-json`, `--continue`) and wired deterministic flat-playlist output behavior in command flow with parser/command tests.
- B10 compatibility follow-up landed: parser now accepts yt-dlp subtitle format flag (`--sub-format` and `-sub-format`) to prevent unknown-flag failures, with regression tests.
- B10 compatibility completion follow-up landed: `--sub-format` now drives subtitle output serialization (`srt`/`vtt`) end-to-end, with transcript formatting utilities promoted into `client` package and CLI consuming package APIs.
- B10 compatibility follow-up landed: parser now accepts yt-dlp subtitle language alias `--sub-langs` (including single-dash `-sub-langs`) mapped to existing `SubLangs` behavior, with parser regression tests.
- B10 compatibility follow-up landed: parser now accepts yt-dlp subtitle write alias `--write-srt` (including `-write-srt`) and maps it to subtitle write flow while forcing `sub-format=srt`.
- B10 compatibility follow-up landed: added `--dump-single-json` mode with yt-dlp-style single-entry payload (`url`, `webpage_url`, `formats`) for tool compatibility (including mpv ytdl hook expectations).
- B10 compatibility follow-up landed: `--print-json` (`-J`, `-j`, `--dump-json`) now emits yt-dlp-style single-entry payload for external tool compatibility, while retaining shared JSON failure payload contract.

### 1.4 Immediate Next Tasks (Strict Order)
1. `[x]` B0. Rebaseline and target-definition reset for Cycle B
2. `[x]` B1. Format selector grammar parity (`-f`)
3. `[x]` B2. Output template and metadata naming parity (`-o` and related fields)
4. `[x]` B3. Download job control parity (retry/continue/abort behavior)
5. `[x]` B4. Playlist/batch execution parity and deterministic reporting
6. `[x]` B5. Subtitle/metadata/post-processing parity for common workflows
7. `[x]` B6. Download archive/idempotency parity
8. `[x]` B7. CLI diagnostics + machine-readable output parity
9. `[x]` B8. Substitute-grade regression matrix and scorecard
10. `[x]` B9. Cycle B closeout and release checklist
11. `[x]` B10. Post-closeout yt-dlp CLI compatibility aliases (`--flat-playlist` and related common flags)

---

## 2. Mission and Scope (Cycle B)

### 2.1 Mission
Make `ytv1` a practical **YouTube-focused** CLI substitute for yt-dlp in daily operator workflows, while preserving package-first architecture.

### 2.2 Scope Boundary
- In scope: YouTube extractor + downloader behavior and CLI operation parity for common yt-dlp usage patterns.
- Out of scope: Multi-site extractor parity across non-YouTube providers.

### 2.3 Non-Negotiable Outcome
- A defined workflow matrix (single video, playlist batch, format selection, subtitle path, archive/idempotent rerun) executes successfully with deterministic behavior and actionable diagnostics.

---

## 3. Execution Tracks

### B0. Rebaseline and Target Definition Reset
- Status: `[x]`
- Goal: Freeze current CLI/operator baseline and define measurable substitute targets.
- Work:
  1. Document current CLI flag behavior vs desired yt-dlp-equivalent workflow expectations.
  2. Define substitute scorecard metrics (workflow pass rate, deterministic outputs, error diagnosability).
  3. Establish test/fixture matrix for Cycle B tracks.
- Target files:
  - `docs/IMPLEMENTATION_PLAN.md`
  - `README.md`
  - `docs/ARCHITECTURE.md`
- Acceptance:
  - Cycle B metrics and test matrix are explicit, concrete, and agreed in docs.

### B1. Format Selector Grammar Parity (`-f`)
- Status: `[x]`
- Goal: Support practical yt-dlp-style format expressions used in real operations.
- Work:
  1. Expand selector parser/evaluator coverage for combined selectors (`A+B`, fallback `/`, filter predicates used in common cases).
  2. Integrate selector resolution into client download selection without pushing logic into CLI.
  3. Add deterministic tie-break and conflict diagnostics.
- Target files:
  - `internal/selector/*`
  - `client/selection.go`
  - `client/download.go`
  - `cmd/ytv1/main.go`
- Acceptance:
  - Common selector recipes execute correctly and are covered by tests.

### B2. Output Template and Metadata Naming Parity
- Status: `[x]`
- Goal: Make output naming predictable for yt-dlp users.
- Work:
  1. Define supported template fields and normalization rules.
  2. Implement deterministic path/file naming behavior for single and playlist cases.
  3. Add collision handling policy and tests.
- Target files:
  - `client/download.go`
  - `client/types.go`
  - `cmd/ytv1/main.go`
  - `README.md`
- Acceptance:
  - Repeated runs produce expected stable paths under identical inputs and options.

### B3. Download Job Control Parity
- Status: `[x]`
- Goal: Align retry/continue/abort semantics with operator expectations.
- Work:
  1. Harden resume/continue behavior and partial file handling policies.
  2. Add configurable error policy for batch (`continue` vs `abort-on-error`).
  3. Expose practical retry/backoff controls through CLI flags mapped to config.
- Target files:
  - `internal/downloader/*`
  - `client/download.go`
  - `internal/cli/parser.go`
  - `cmd/ytv1/main.go`
- Acceptance:
  - Interrupted and transient-failure scenarios recover predictably with tested policies.

### B4. Playlist/Batch Execution Parity
- Status: `[x]`
- Goal: Make multi-item runs deterministic and operable.
- Work:
  1. Define per-item status accounting and final summary output.
  2. Ensure item-level failures do not corrupt global progress state.
  3. Add stable ordering and replay-safe behavior.
- Target files:
  - `cmd/ytv1/main.go`
  - `client/playlist_transcript.go`
  - `client/*_test.go`
- Acceptance:
  - Batch runs provide clear totals and deterministic per-item results.

### B5. Subtitle/Metadata/Post-processing Parity (Common Paths)
- Status: `[x]`
- Goal: Close practical gaps for frequent subtitle and conversion workflows.
- Work:
  1. Align subtitle language selection/fallback behavior for common flags.
  2. Validate MP3/transcode path behavior and failure reporting.
  3. Ensure metadata outputs are consistent with CLI expectations in common flows.
- Target files:
  - `client/playlist_transcript.go`
  - `client/download_mp3_test.go`
  - `internal/cli/parser.go`
  - `README.md`
- Acceptance:
  - Core subtitle and mp3 workflows are reproducible and tested.

### B6. Download Archive/Idempotency Parity
- Status: `[x]`
- Goal: Avoid re-downloading already completed items in repeat operations.
- Work:
  1. Introduce archive persistence model and skip policy.
  2. Wire CLI option(s) to archive backend without polluting extractor logic.
  3. Add tests for rerun idempotency and corruption handling.
- Target files:
  - `cmd/ytv1/main.go`
  - `internal/cli/parser.go`
  - `client/download.go`
  - `client/*_test.go`
- Acceptance:
  - Second run with archive enabled skips already-completed items deterministically.

### B7. CLI Diagnostics and Machine-readable Output Parity
- Status: `[x]`
- Goal: Improve automation usability and operator triage speed.
- Work:
  1. Standardize structured diagnostics payloads for extraction/download failures.
  2. Improve `--print-json` contract coverage for automation scripts.
  3. Add explicit exit-code policy by failure class.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/diagnostics_test.go`
  - `client/errors.go`
  - `README.md`
- Acceptance:
  - Automation can rely on stable JSON/error semantics and documented exit codes.

### B8. Substitute-grade Regression Matrix and Scorecard
- Status: `[x]`
- Goal: Replace anecdotal confidence with quantified substitute readiness.
- Work:
  1. Build representative workflow matrix (single, playlist, selectors, subtitles, archive rerun).
  2. Add live-gated and fixture-gated tests per workflow class.
  3. Publish scorecard thresholds and measured results in docs.
- Target files:
  - `client/e2e_integration_test.go`
  - `cmd/ytv1/*_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
  - `README.md`
- Acceptance:
  - Readiness scorecard is reproducible from test commands and documented thresholds.

### B9. Cycle B Closeout
- Status: `[x]`
- Goal: Close cycle with explicit pass/fail truth and residual-gap register.
- Work:
  1. Run full tests + live-gated matrix.
  2. Mark all tracks complete/blocked with evidence.
  3. Document remaining gaps by severity and owner.
- Target files:
  - `docs/IMPLEMENTATION_PLAN.md`
  - `README.md`
- Acceptance:
  - Plan state matches verified runtime reality and includes concrete residual risks.

### B10. Post-closeout yt-dlp CLI Compatibility Aliases
- Status: `[x]`
- Goal: Reduce operator friction by accepting common yt-dlp CLI aliases without breaking package-first boundaries.
- Work:
  1. Add parser support for common compatibility aliases (playlist/error-handling/json flags).
  2. Implement minimal deterministic behavior for `--flat-playlist` in playlist flows.
  3. Add parser/command-layer regression tests for new aliases and playlist behavior.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
- Acceptance:
  - Added aliases parse deterministically and `go test ./...` remains green.

---

## 4. Public API Contract

1. Preserve `client.New`, `GetVideo`, `GetFormats`, `ResolveStreamURL` behavior.
2. Prefer additive config/events over breaking signatures.
3. Maintain `errors.Is` compatibility for sentinel errors.

---

## 5. Global Done Criteria (Cycle B)

Cycle B is complete only when all are true:
1. `B0-B9` are `[x]` or explicitly `[!]` with reason.
2. `go test ./...` is green.
3. Live-gated workflow matrix passes at documented threshold.
4. Remaining gaps are documented with exact blocker classes and impact.

---

## 6. Change Log (Plan)

- `2026-02-16`: Replaced completed deep-port migration plan (`R0-R11`) with new execution cycle (`B0-B9`) focused on YouTube CLI substitute readiness for yt-dlp-style operations.
- `2026-02-16`: Marked `B0` as in-progress to begin gap matrix and target-definition reset using current repository baseline.
- `2026-02-16`: Completed `B0` by synchronizing Cycle B scorecard/matrix contract across `docs/IMPLEMENTATION_PLAN.md`, `README.md`, and `docs/ARCHITECTURE.md`; moved `B1` to in-progress.
- `2026-02-16`: B1 increment: wired CLI `-f` passthrough into `client.DownloadOptions.FormatSelector`, preserved mode aliases (`best/mp4/mp3/...`), added numeric itag parsing, and added command-layer tests for selector/itag/mp3 mapping.
- `2026-02-16`: B1 increment: hardened selector behavior by adding `width` filter type, strict extension mapping (`audio/mp4` -> `m4a`), AV-first ranking for generic `best`, and new `internal/selector` tests covering `bestvideo+bestaudio` ext constraints and `/` fallback selection.
- `2026-02-16`: B1 increment: added worst-media selection handling (`worstvideo`/`worstaudio`) and base-token predicate parsing via modifier grammar (e.g. `fps!=60` outside brackets), with selector tests for worst and `!=` filter scenarios.
- `2026-02-16`: Completed `B1` by unifying selector parse/no-match failures under typed `NoPlayableFormatsDetailError` (`selector`, `selection_reason`) and surfacing selector-aware CLI remediation hints; moved `B2` to in-progress.
- `2026-02-16`: Completed `B2` by implementing deterministic output template token rendering (`id/title/uploader/ext/itag`) with filesystem-safe normalization for single/merge downloads and adding output-template tests; moved `B3` to in-progress.
- `2026-02-16`: B3 increment: added CLI batch-control and retry/resume flags, wired them into `main` processing flow and `ToClientConfig` transport overrides, and added tests for abort behavior and retry override mapping.
- `2026-02-16`: Completed `B3` by wiring resume control (`--no-continue`) into download path, validating batch abort/continue loop semantics through unit tests, and retaining retry/backoff overrides as explicit CLI operator controls; moved `B4` to in-progress.
- `2026-02-16`: Completed `B4` by adding deterministic playlist run accounting (`total/succeeded/failed/aborted`), explicit item-failure collection, and abort-on-error cutover behavior in command-layer tests; moved `B5` to in-progress.
- `2026-02-16`: Completed `B5` by wiring CLI subtitle workflows (`--write-subs/--write-auto-subs/--sub-lang`) into transcript-to-SRT output with deterministic naming, applying explicit language auto/manual preference policy in subtitle selection, and expanding tests/remediation hints for common subtitle/mp3 paths; moved `B6` to in-progress.
- `2026-02-16`: Completed `B6` by adding archive persistence (`--download-archive`) with rerun skip semantics, append-on-success recording, and corruption-tolerant archive loading; added command-layer tests for idempotent rerun and archive integrity handling, and moved `B7` to in-progress.
- `2026-02-16`: Completed `B7` by standardizing machine-readable CLI failure JSON in `--print-json` mode, adding package-level error category classification + command-layer exit-code policy, extending diagnostics tests, and documenting exit-code contract; moved `B8` to in-progress.
- `2026-02-16`: Completed `B8` by adding explicit fixture workflow-matrix tests (`TestWorkflowMatrix_FixtureCoverage`), documenting reproducible fixture/live matrix commands, and publishing fixture-gated scorecard measurements from current test evidence; moved `B9` to in-progress.
- `2026-02-16`: Completed `B9` closeout by running full suite (`go test ./...`) and live-gated matrix (`YTV1_E2E=1 go test ./client -run TestE2E_ -count=1 -timeout 8m`) successfully, marking all tracks complete, and documenting residual risks with severity/owner.
- `2026-02-16`: Added CLI compatibility alias `-J` (short form of `--print-json`) and parser regression test (`TestParseFlags_ShortJEnablesPrintJSON`).
- `2026-02-16`: Completed `B10` by adding yt-dlp compatibility aliases for playlist/error/json/resume flows (`--flat-playlist`, `--extract-flat`, `--no-playlist`, `--yes-playlist`, `--ignore-errors`/`-i`, `-j`, `--dump-json`, `--continue`) and flat-playlist deterministic output behavior with parser + command-layer regression tests.
- `2026-02-16`: B10 compatibility follow-up: added parser support for yt-dlp subtitle format flag (`--sub-format` and `-sub-format`) and parser regression test coverage.
- `2026-02-16`: B10 compatibility completion follow-up: wired `--sub-format` into subtitle write path (`best/srt/vtt` preference handling), added package-level transcript serialization API in `client` (`ResolveSubtitleOutputFormat`, `WriteTranscript`), and updated CLI/tests to consume it.
- `2026-02-16`: B10 compatibility follow-up: added subtitle language alias support (`--sub-langs`, `-sub-langs`) mapped to existing subtitle language parsing flow, with parser regression coverage.
- `2026-02-16`: B10 compatibility follow-up: added subtitle write alias support (`--write-srt`, `-write-srt`) mapped to `WriteSubs` and forced `SubFormat=srt`, with parser regression coverage.
- `2026-02-16`: B10 compatibility follow-up: added `--dump-single-json` parser/emit path and yt-dlp-style payload serialization with CLI regression tests to improve external tool interoperability.
- `2026-02-16`: B10 compatibility follow-up: aligned `--print-json` output path with `--dump-single-json` yt-dlp-style payload emission so callers that pass only `-J/--print-json` (e.g. mpv ytdl-hook variants) receive a playable `url` field.

---

## 7. Residual Risk Register (Post-Closeout)

1. `Medium` - Upstream YouTube behavior drift may break live extraction/download without code changes.
   owner: `internal/orchestrator`, `internal/playerjs`, `internal/challenge` maintainers.
2. `Low` - CLI substitute claim is YouTube-scoped; multi-site parity is intentionally out of scope.
   owner: product scope maintainers (`docs/IMPLEMENTATION_PLAN.md`, `README.md`).
3. `Low` - Live-gated confidence depends on periodic reruns; fixture matrix alone cannot detect all upstream runtime shifts.
   owner: release/checklist maintainers (`client/e2e_integration_test.go` + CI operators).
