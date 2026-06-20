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
- `2026-06-20`

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
- B11 completion increment landed: `-F` format list now emits explicit `Note` labels (`audio only`/`video only`/`av`) so operators can pick direct audio formats without relying on `0x0` inference.
- B12 completion increment landed: playlist flows now suppress human stdout in JSON modes, alias reconciliation honors argument order for conflicting flags, and startup/config failures emit structured JSON diagnostics under `--print-json`.
- B13 upstream drift response landed: ported latest yt-dlp YouTube client policy changes affecting audio-only extraction (`android_vr -> web_safari` defaults, `web_creator` fallback profile, embedded client request shaping, PO token sanitization) and added an audio-only live regression gate.
- B14 completion increment landed: current real `base.js` fixture now exercises runtime `n` challenge solving, runtime export discovery accepts obfuscated URL wrapper constructors beyond `g.Mj`, and invalid unchanged/sentinel `n` outputs are rejected before success/caching.
- B15 completion increment landed: CLI now supports yt-dlp-style `--write-info-json`, writing deterministic `.info.json` sidecars from the existing single-entry metadata payload and `-o` template naming.
- B16 completion increment landed: CLI now supports yt-dlp-style `--write-description`, writing deterministic `.description` sidecars from extracted metadata and `-o` template naming.
- B17 completion increment landed: `client.VideoInfo` now exposes selected thumbnail metadata, and CLI supports yt-dlp-style `--write-thumbnail` sidecar download with deterministic `-o` template naming.
- B18 completion increment landed: CLI now supports yt-dlp-style `--playlist-items` filtering for flat and download playlist flows with 1-based item/range selectors.
- B19 completion increment landed: CLI now accepts yt-dlp-style `--playlist-start`/`--playlist-end` aliases and maps them into the shared 1-based playlist selector path.
- B20 completion increment landed: CLI now supports deterministic yt-dlp-style `--playlist-reverse` ordering after item selection for flat and download playlist flows.
- B21 completion increment landed: CLI now supports yt-dlp-style `--skip-playlist-after-errors`, stopping playlist processing after a configured failure threshold with explicit skipped count in summaries.
- B22 completion increment landed: CLI now accepts yt-dlp short alias `-I` for `--playlist-items`.
- B23 completion increment landed: `--playlist-items` now accepts yt-dlp backward-compatible positive `START-STOP` ranges alongside existing colon ranges.
- B24 completion increment landed: `--playlist-items` now supports yt-dlp-style negative single indices and negative colon-range bounds that count from the right.
- B25 completion increment landed: `--playlist-items` now supports positive `START:STOP:STEP` selectors for deterministic playlist sampling.
- B26 completion increment landed: `--playlist-items` now supports negative step selectors such as `5:1:-2` and `::-1`, preserving requested selector order with duplicate suppression.
- B27 completion increment landed: CLI now supports yt-dlp-style `--playlist-random` ordering for playlist workflows, with `--playlist-reverse` retaining yt-dlp precedence when both are set.
- B28 completion increment landed: playlist processing now preserves original item indexes and renders `%(playlist_index)s` / `%(playlist_autonumber)s` before per-item downloads and sidecar writes.
- B29 completion increment landed: playlist processing now renders filename-safe `%(playlist_id)s` and `%(playlist_title)s` before per-item downloads and sidecar writes.
- B30 completion increment landed: playlist processing now renders zero-padded `%(playlist_count)s` from the selected per-run playlist item set.
- B31 completion increment landed: playlist extraction now exposes owner/channel metadata and CLI templates render yt-dlp playlist owner/channel tokens.
- B32 completion increment landed: `--break-on-existing` now stops URL batch and playlist processing cleanly on archive hits while default archive skips still continue.
- B33 completion increment landed: `--force-write-archive` aliases now record archive IDs for successful JSON/list/skip-download metadata workflows.
- B34 completion increment landed: `--max-downloads` now counts completed download records and stops batch/playlist processing cleanly at the configured limit.
- B35 completion increment landed: `--no-download-archive` now disables archive use with last-flag-wins behavior against `--download-archive`.
- B36 completion increment landed: `--no-download` now maps to the existing `--skip-download` no-download workflow.
- B37 completion increment landed: subtitle parsing now accepts yt-dlp aliases `--write-automatic-subs` and `--srt-langs`.
- B38 completion increment landed: subtitle parsing now accepts negative write aliases and preserves last-flag-wins behavior for subtitle write modes.
- B39 completion increment landed: `--list-subs` now lists available manual/automatic subtitle tracks and exits before downloads.
- B40 completion increment landed: `--all-subs` now expands subtitle writes across available manual/automatic track languages with deterministic duplicate suppression.
- B41 completion increment landed: `--get-thumbnail` now prints the selected thumbnail URL and exits before downloads.
- B42 completion increment landed: `--no-write-thumbnail` now disables thumbnail sidecar writes with last-flag-wins behavior against `--write-thumbnail`.
- B43 completion increment landed: simple metadata `--get-*` flags now print title/id/description/duration in request order and exit before downloads.
- B44 completion increment landed: `--get-format` now prints deterministic selected format summaries using the existing practical selector path.
- B45 completion increment landed: `--get-filename` now prints the predicted output path for selected numeric and merged formats while respecting existing `-o` template tokens.
- B46 completion increment landed: `--get-url` now resolves and prints selected direct media URL(s), including merged selections, through the existing format selector and stream URL resolver paths.
- B47 completion increment landed: yt-dlp-style `-s`/`--simulate` aliases now map to the existing no-download workflow.
- B48 completion increment landed: `-q`/`--quiet` now suppresses human progress/status output while preserving explicit result/list outputs.
- B49 completion increment landed: negative `--no-simulate` and `--no-quiet` aliases now use last-flag-wins behavior against their positive flags.
- B50 completion increment landed: practical `-O`/`--print` metadata field/template output now supports repeated video-stage field/template printing with implied quiet/simulate behavior.
- B51 completion increment landed: short `-g` now aliases `--get-url` and participates in combined get flag ordering.
- B52 completion increment landed: practical `--print-to-file TEMPLATE FILE` now appends rendered print output while preserving URL arguments and implied quiet/simulate behavior.
- B53 completion increment landed: `--sub-langs all` now expands through available subtitle tracks and supports simple exclusions such as `all,-live_chat`.
- B54 completion increment landed: explicit subtitle language lists now honor negative `-language` exclusions before transcript requests.
- B55 completion increment landed: `--convert-subs`, `--convert-sub`, and `--convert-subtitles` now map to the subtitle output format preference with later-flag precedence.
- B56 completion increment landed: common date output template tokens now render across predicted filenames, sidecar paths, and print templates.
- B57 completion increment landed: common video uploader/channel identifier output template tokens now render across predicted filenames, sidecar paths, and print templates.
- B58 completion increment landed: selected-format template tokens now expose format id, resolution, dimensions, and FPS from the chosen format.
- B59 completion increment landed: selected-format bitrate template tokens now expose total, video, and audio bitrate values in kbit/s.
- B60 completion increment landed: selected-format protocol template token now renders for single and merged selections.
- B61 completion increment landed: selected-format codec template tokens now render video/audio codec values from MIME codec metadata.
- B62 completion increment landed: `--print`/`--print-to-file` now render webpage/original URL fields from the input URL.
- B63 completion increment landed: `-a`/`--batch-file` now loads URL lists with blank/comment skipping and `--no-batch-file` override support.
- B64 completion increment landed: `-P`/`--paths` now roots default downloads, relative output templates, predicted filenames, subtitles, and metadata sidecars under a configured home output directory.
- B65 completion increment landed: `--id` now acts as an output-template shortcut for video-ID basenames across downloads, predicted filenames, and sidecars, including `-P` output directory composition.
- B66 completion increment landed: `--restrict-filenames` now enables ASCII-safe output template token normalization across predicted filenames, downloads, print-to-file paths, subtitles, and metadata sidecars.
- B67 completion increment landed: `--trim-filenames` now limits rendered output basenames for predicted filenames and metadata sidecars while preserving directories and extensions.
- B68 completion increment landed: actual CLI downloads now receive the same concrete output path as `--get-filename`, including advanced CLI tokens and restrict/trim filename policies.
- B69 completion increment landed: `--no-overwrites` / `--force-overwrites` now control whether CLI downloads and metadata sidecars clobber existing files.
- B70 completion increment landed: overwrite aliases `-w`, `--yes-overwrites`, and `--no-force-overwrites` now match yt-dlp precedence, and final force-overwrite policy disables resume.
- B71 completion increment landed: `--trim-file-names` now aliases `--trim-filenames` with shared value parsing and precedence.
- B72 completion increment landed: CLI media downloads now support yt-dlp-style `.part` temporary files by default with `--part` / `--no-part` controls and resume from existing part files.
- B73 completion increment landed: `--mtime` / `--no-mtime` now control post-download media file modification time using available YouTube upload/publish dates.
- B74 completion increment landed: `--write-link` / `--write-url-link` now write deterministic Windows `.url` internet shortcut sidecars with shared output-template and overwrite policies.
- B75 completion increment landed: `--write-webloc-link` and `--write-desktop-link` now create deterministic `.webloc` and `.desktop` internet shortcut sidecars with shared output-template policies.
- B76 completion increment landed: `-x` / `--extract-audio` and `--audio-format` now map common yt-dlp audio extraction workflows onto existing audio-only and MP3 modes.
- B77 completion increment landed: `--no-write-info-json` and `--no-write-description` now disable their sidecar workflows with last-flag-wins behavior.
- B78 completion increment landed: playlist processing with `--write-info-json` now writes a playlist-level `.info.json`, controlled by `--write-playlist-metafiles` / `--no-write-playlist-metafiles`.
- B79 completion increment landed: `--audio-quality` now flows from CLI parsing into MP3 download options and transcode metadata.
- B80 completion increment landed: `--embed-metadata` / `--add-metadata` and negative aliases now control CLI ffmpeg merge metadata embedding while preserving library defaults.
- B81 completion increment landed: `--postprocessor-args` / `--ppa` now pass practical default/FFmpeg/Merger scoped args into ffmpeg merge commands.
- B82 completion increment landed: `--merge-output-format` now controls predicted filenames, output templates, and actual merged output paths.
- B83 completion increment landed: `-k` / `--keep-video` and `--no-keep-video` now control merged-download intermediate file retention.
- B84 completion increment landed: common `--remux-video FORMAT` requests now derive merged-output container selection, with `--merge-output-format` taking precedence.
- B85 completion increment landed: `--post-overwrites` / `--no-post-overwrites` now control existing merged/MP3 post-processed output skips.
- B86 completion increment landed: `-r` / `--limit-rate` / `--rate-limit` now parse byte-rate values and throttle direct HTTP stream copies.
- B87 completion increment landed: `--sleep-interval` / `--min-sleep-interval`, `--max-sleep-interval`, and `--sleep-subtitles` now pace media and subtitle downloads.
- B88 completion increment landed: `--sleep-requests` now paces video, playlist, and subtitle-track extraction/list requests.
- B89 completion increment landed: `--buffer-size`, `--http-chunk-size`, and resize-buffer aliases now configure direct download chunk sizing.
- B90 completion increment landed: `-N` / `--concurrent-fragments` and unavailable-fragment skip/abort aliases now configure HLS/DASH fragment transport.
- B91 completion increment landed: `--fragment-retries` now configures HLS/DASH fragment retry counts and overrides generic `--retries` for download transport only.
- B92 completion increment landed: `--retry-sleep [TYPE:]EXPR` now maps practical retry sleep expressions onto download and metadata transport backoff.
- B93 completion increment landed: `--socket-timeout SECONDS` now maps to package request timeout and can shorten broader caller deadlines.
- B94 completion increment landed: `--source-address`, `-4`, and `-6` now configure outbound source-address binding for the default HTTP client.
- B95 completion increment landed: `--extractor-retries` now configures metadata/extractor retry counts separately from download retries.
- B96 completion increment landed: `--no-progress`, `--progress`, and `--newline` now control CLI progress/status output compatibility.
- B97 completion increment landed: `--no-check-certificates` now disables TLS certificate verification for the default HTTP client when explicitly requested.
- B98 completion increment landed: `--user-agent`, `--referer`, and repeatable `--add-headers` now populate request headers through existing package plumbing.
- B99 completion increment landed: package-first network configuration is now documented and tested through direct `client.Config` usage, not only CLI adapter mapping.
- B100 completion increment landed: package `DownloadTransportConfig` now supports throttled-rate detection and retry for direct HTTP downloads.
- B101 completion increment landed: writer downloads now buffer retry attempts and only commit the successful body after throttled-rate recovery.
- B102 completion increment landed: package throttled-rate settings now propagate into HLS/DASH fragment downloader transport and retry sustained low-speed fragment reads.
- B103 completion increment landed: resumed range append retries now truncate back to the original resume offset before retrying.
- B104 completion increment landed: full-rewrite file downloads now retry throttled attempts by truncating and streaming directly to the destination file.
- B105 completion increment landed: package `DownloadTransportConfig` now supports file access retries for transient `.part` finalization failures.
- B106 completion increment landed: HLS/DASH package downloads now honor part-file finalization and file access retries.
- B107 completion increment landed: package download file open/create/remove operations now use file access retries.
- B108 completion increment landed: package file access retries now default to yt-dlp's 3 attempts and CLI exposes `--file-access-retries`.
- B109 completion increment landed: playlist item selector behavior moved from `cmd/ytv1/main.go` into package API.
- B110 completion increment landed: output template rendering and selected-format token calculation moved into package API.
- B111 completion increment landed: output filename prediction policy moved from `cmd/ytv1/main.go` into package API.
- B112 completion increment landed: sidecar output path policy moved from `cmd/ytv1/main.go` into package API.
- B113 completion increment landed: subtitle language selection policy moved from `cmd/ytv1/main.go` into package API.
- B114 completion increment landed: download archive persistence moved from `cmd/ytv1/main.go` into package API.
- B115 completion increment landed: playlist ordering helpers moved from `cmd/ytv1/main.go` into package API.
- B116 completion increment landed: internet shortcut sidecar body rendering moved from `cmd/ytv1/main.go` into package API.
- B117 completion increment landed: yt-dlp-style JSON payload builders moved from `cmd/ytv1/main.go` into package API.
- B118 completion increment landed: selected-format planning moved from `cmd/ytv1/main.go` into package API.
- B119 completion increment landed: selected-format summary and format-list note rendering moved from `cmd/ytv1/main.go` into package API.
- B120 completion increment landed: metadata print field/template rendering moved from `cmd/ytv1/main.go` into package API.
- B121 completion increment landed: media file modification-time derivation moved from `cmd/ytv1/main.go` into package API.
- B122 completion increment landed: thumbnail sidecar download transport moved from `cmd/ytv1/main.go` into package API.
- B123 completion increment landed: description sidecar file writing moved from `cmd/ytv1/main.go` into package API.
- B124 completion increment landed: shortcut sidecar file writing moved from `cmd/ytv1/main.go` into package API.
- B125 completion increment landed: moved reusable download option construction, output-template composition, remux/merge extension resolution, selected stream URL resolution, info JSON sidecar writing, and playlist metadata adaptation from `cmd/ytv1/main.go` into package-level `client` APIs.
- B126 completion increment landed: moved requested subtitle sidecar workflow, playlist item run accounting/template context, and list-output rendering from `cmd/ytv1/main.go` into package APIs.
- B127 completion increment landed: moved metadata/get/print rendering and print-to-file append workflow from `cmd/ytv1/main.go` into package APIs.
- B128 completion increment landed: moved diagnostics/remediation and lifecycle event formatting from `cmd/ytv1/main.go` into package APIs.
- B129 completion increment landed: split remaining CLI adapter helpers and video workflow out of `cmd/ytv1/main.go`, leaving `main.go` as a thin entrypoint/run-loop wrapper over package and command adapters.
- B130 completion increment landed: explicit video-only `video/mp4; codecs="avc1..."` entries no longer inherit progressive audio flags, while codec-less progressive fallback remains intact; verified against `oaevSXpWhdo` and `go test ./...`.
- B131 completion increment landed: merged PR #9 security hardening into `dev` while preserving dev-first flow, bounded response/body reads, hardened player JS execution/cache behavior, validated encrypted HLS padding, bounded DASH concurrent buffering without rejecting long static streams, hardened ffmpeg merge inputs/metadata, and added regression coverage for JS timeout and long DASH segment batching.
- B132 completion increment landed: default downloads preserve highest video+audio selections instead of downgrading ciphered high-quality formats to low non-ciphered alternatives, missing muxer support now fails explicitly instead of silently falling back to low progressive files, default media filenames remain `%(title)s [%(id)s].%(ext)s`, live selection for `8IY44RZHyw8` resolves to 4K `315+251`, and `go test ./...` is green.
- B133 completion increment landed: default unauthenticated extraction now prefers the iOS client before MWEB to avoid current POT-less MWEB high-quality direct media 403s, while preserving 4K `315+251` selection for `8IY44RZHyw8`; live one-byte Range checks return `206` for both selected video and audio URLs, and `go test ./...` is green.
- B134 completion increment landed: ported current yt-dlp YouTube multi-client extraction behavior from upstream `9ae7df9a22b29e2f81825230c9bba7d444190de0` by merging successful client streaming formats in deterministic order, preserving per-format source client metadata, replacing metadata-only duplicate itags with later playable URLs, and aligning the default unauthenticated order with current yt-dlp bypass clients first while retaining iOS as a ytv1 high-quality fallback; live `8IY44RZHyw8` selection remains 4K `315+251`, selected URLs return `206`, and `go test ./...` is green.

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
12. `[x]` B11. Format list UX parity (`-F` note column with explicit audio/video-only labels)
13. `[x]` B12. Post-closeout CLI contract fixes (playlist JSON, alias precedence, startup JSON diagnostics)
14. `[x]` B13. Upstream YouTube drift response from latest yt-dlp client/POT changes
15. `[x]` B14. Current player JS challenge drift hardening
16. `[x]` B15. Metadata sidecar parity (`--write-info-json`)
17. `[x]` B16. Description sidecar parity (`--write-description`)
18. `[x]` B17. Thumbnail sidecar parity (`--write-thumbnail`)
19. `[x]` B18. Playlist item selection parity (`--playlist-items`)
20. `[x]` B19. Playlist range aliases (`--playlist-start`, `--playlist-end`)
21. `[x]` B20. Playlist reverse ordering (`--playlist-reverse`)
22. `[x]` B21. Playlist failure threshold (`--skip-playlist-after-errors`)
23. `[x]` B22. Playlist item short alias (`-I`)
24. `[x]` B23. Playlist item hyphen ranges (`START-STOP`)
25. `[x]` B24. Playlist negative indices (`--playlist-items -N`)
26. `[x]` B25. Playlist positive step selectors (`START:STOP:STEP`)
27. `[x]` B26. Playlist negative step selectors (`START:STOP:-STEP`)
28. `[x]` B27. Playlist random ordering (`--playlist-random`)
29. `[x]` B28. Playlist output template token (`%(playlist_index)s`)
30. `[x]` B29. Playlist metadata output template tokens
31. `[x]` B30. Playlist count output template token
32. `[x]` B31. Playlist owner output template tokens
33. `[x]` B32. Archive break-on-existing behavior
34. `[x]` B33. Force-write archive for simulated workflows
35. `[x]` B34. Max downloads cutoff behavior
36. `[x]` B35. No-download-archive override alias
37. `[x]` B36. No-download alias
38. `[x]` B37. Subtitle compatibility aliases
39. `[x]` B38. Negative subtitle compatibility aliases
40. `[x]` B39. Subtitle listing workflow
41. `[x]` B40. All subtitles workflow
42. `[x]` B41. Get thumbnail URL workflow
43. `[x]` B42. No-write-thumbnail override alias
44. `[x]` B43. Simple metadata get flags
45. `[x]` B44. Get selected format summary
46. `[x]` B45. Get filename workflow
47. `[x]` B46. Get direct media URL workflow
48. `[x]` B47. Simulate/no-download aliases
49. `[x]` B48. Quiet output mode
50. `[x]` B49. Negative simulate/quiet aliases
51. `[x]` B50. Print field/template workflow
52. `[x]` B51. Short get-url alias
53. `[x]` B52. Print-to-file workflow
54. `[x]` B53. Subtitle language `all` selector
55. `[x]` B54. Explicit subtitle language exclusions
56. `[x]` B55. Convert subtitles format aliases
57. `[x]` B56. Date output template tokens
58. `[x]` B57. Video uploader/channel output template tokens
59. `[x]` B58. Selected format output template tokens
60. `[x]` B59. Selected format bitrate template tokens
61. `[x]` B60. Selected format protocol template token
62. `[x]` B61. Selected format codec template tokens
63. `[x]` B62. Webpage/original URL print fields
64. `[x]` B63. Batch file URL input
65. `[x]` B64. Output paths home directory
66. `[x]` B65. ID filename shortcut
67. `[x]` B66. Restricted filename mode
68. `[x]` B67. Trim filename length mode
69. `[x]` B68. Concrete CLI download output path
70. `[x]` B69. Overwrite policy flags
71. `[x]` B70. Overwrite alias and resume precedence
72. `[x]` B71. Trim filename alias
73. `[x]` B72. Part file download workflow
74. `[x]` B73. Media file mtime workflow
75. `[x]` B74. Internet shortcut sidecars
76. `[x]` B75. Webloc and desktop shortcut sidecars
77. `[x]` B76. Extract audio workflow
78. `[x]` B77. Negative metadata sidecar aliases
79. `[x]` B78. Playlist metadata sidecar controls
80. `[x]` B79. Audio quality transcode option
81. `[x]` B80. Embed metadata controls
82. `[x]` B81. Postprocessor args for ffmpeg merge
83. `[x]` B82. Merge output format option
84. `[x]` B83. Keep intermediate video workflow
85. `[x]` B84. Remux video container option
86. `[x]` B85. Post-processed overwrite policy
87. `[x]` B86. Direct download rate limiting
88. `[x]` B87. Download and subtitle sleep intervals
89. `[x]` B88. Extraction request sleep interval
90. `[x]` B89. Download buffer and HTTP chunk sizing
91. `[x]` B90. Fragment download controls
92. `[x]` B91. Fragment retry count
93. `[x]` B92. Retry sleep expression option
94. `[x]` B93. Socket timeout option
95. `[x]` B94. Source address option
96. `[x]` B95. Extractor retry count
97. `[x]` B96. Progress output controls
98. `[x]` B97. No-check-certificates option
99. `[x]` B98. Request header controls
100. `[x]` B99. Package-first network config hardening
101. `[x]` B100. Package throttled-rate detection
102. `[x]` B101. Throttled retry writer integrity
103. `[x]` B102. Fragment throttled-rate propagation
104. `[x]` B103. Resume append retry integrity
105. `[x]` B104. Full rewrite retry integrity
106. `[x]` B105. Package file access retries
107. `[x]` B106. HLS/DASH part finalization retries
108. `[x]` B107. Package file open/remove retries
109. `[x]` B108. File access retry defaults and CLI adapter
110. `[x]` B109. Thin CLI refactor for playlist item selection
111. `[x]` B110. Thin CLI refactor for output template tokens
112. `[x]` B111. Thin CLI refactor for output filename prediction
113. `[x]` B112. Thin CLI refactor for sidecar output paths
114. `[x]` B113. Thin CLI refactor for subtitle language selection
115. `[x]` B114. Thin CLI refactor for download archive
116. `[x]` B115. Thin CLI refactor for playlist ordering
117. `[x]` B116. Thin CLI refactor for shortcut sidecar body rendering
118. `[x]` B117. Thin CLI refactor for yt-dlp JSON payload builders
119. `[x]` B118. Thin CLI refactor for selected format planning
120. `[x]` B119. Thin CLI refactor for format summary rendering
121. `[x]` B120. Thin CLI refactor for metadata print rendering
122. `[x]` B121. Thin CLI refactor for media mtime policy
123. `[x]` B122. Thin CLI refactor for thumbnail download
124. `[x]` B123. Thin CLI refactor for description sidecar writing
125. `[x]` B124. Thin CLI refactor for shortcut sidecar writing
126. `[x]` B125. Thin CLI refactor for reusable download/output planning helpers
127. `[x]` B126. Thin CLI refactor for subtitle, playlist-run, and list-output workflows
128. `[x]` B127. Thin CLI refactor for metadata print workflow
129. `[x]` B128. Thin CLI refactor for diagnostics and lifecycle formatting
130. `[x]` B129. Thin `cmd/ytv1/main.go` entrypoint split
131. `[x]` B130. Explicit codec media-flag correction for video-only MP4 formats
132. `[x]` B131. PR #9 security hardening integration on `dev`
133. `[x]` B132. Default best-quality downloads and yt-dlp-style default filename
134. `[x]` B133. Default client order mitigation for high-quality media 403s
135. `[x]` B134. Latest yt-dlp multi-client YouTube bypass alignment

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

### B12. Post-closeout CLI Contract Fixes
- Status: `[x]`
- Goal: Repair automation-facing CLI regressions without changing package-first boundaries.
- Work:
  1. Keep playlist stdout machine-readable in JSON modes.
  2. Make alias parsing honor argument order / last-flag-wins semantics.
  3. Route startup/configuration failures through the structured JSON diagnostics contract when `--print-json` is active.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/*_test.go`
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
- Acceptance:
  - JSON modes emit JSON-only stdout, alias precedence is deterministic by argument order, and startup failures are covered by tests.

### B13. Upstream YouTube Drift Response
- Status: `[x]`
- Goal: Restore live extraction resilience after recent YouTube/yt-dlp client-policy changes, especially audio-only format selection failures.
- Work:
  1. Compare latest local `D:\yt-dlp\yt_dlp\extractor\youtube` client/POT changes.
  2. Port relevant behavior without changing public package API.
  3. Add focused regression tests for default client order, new client profile availability, embedded request shaping, and PO token sanitization.
- Target files:
  - `internal/innertube/*`
  - `internal/policy/*`
  - `internal/orchestrator/*`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `go test ./...` remains green and the plan records the exact yt-dlp drift response.
  - Live-gated audio-only smoke passes for the default E2E video.

### B14. Current Player JS Challenge Drift Hardening
- Status: `[x]`
- Goal: Keep signature and `n` challenge solving resilient against current YouTube player JavaScript shapes.
- Work:
  1. Add a real current `base.js` fixture regression for `n` challenge solving.
  2. Align runtime fallback validation with yt-dlp behavior by rejecting unchanged/sentinel `n` outputs.
  3. Harden the Go runtime shim/export discovery only as needed for the fixture.
- Target files:
  - `internal/playerjs/*`
  - `internal/playerjs/testdata/*`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Current real `base.js` fixture produces a valid transformed `n` result and `go test ./...` remains green.

### B15. Metadata Sidecar Parity (`--write-info-json`)
- Status: `[x]`
- Goal: Support a common yt-dlp metadata workflow by writing deterministic `.info.json` sidecar files.
- Work:
  1. Add CLI parser support for `--write-info-json`.
  2. Reuse the existing yt-dlp-style JSON payload builder for file output.
  3. Derive sidecar paths from `-o` templates with safe token rendering and deterministic `.info.json` suffix.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--write-info-json --skip-download` writes a valid sidecar without human JSON stdout, template naming is deterministic, and `go test ./...` remains green.

### B16. Description Sidecar Parity (`--write-description`)
- Status: `[x]`
- Goal: Support yt-dlp's common description sidecar workflow using already-extracted package metadata.
- Work:
  1. Add CLI parser support for `--write-description`.
  2. Write deterministic `.description` sidecar files after extraction and before download/skip handling.
  3. Reuse template token rendering rules for stable names.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--write-description --skip-download` writes the extracted description to a deterministic sidecar path and `go test ./...` remains green.

### B17. Thumbnail Sidecar Parity (`--write-thumbnail`)
- Status: `[x]`
- Goal: Support common yt-dlp thumbnail archival workflows while preserving package-first metadata access.
- Work:
  1. Add public thumbnail metadata fields to `client.VideoInfo` without breaking existing APIs.
  2. Choose the highest-resolution thumbnail from videoDetails/microformat metadata.
  3. Add CLI parser/output support for `--write-thumbnail` with deterministic template naming.
- Target files:
  - `client/types.go`
  - `client/client.go`
  - `client/*_test.go`
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `GetVideo` exposes selected thumbnail metadata, `--write-thumbnail --skip-download` writes the thumbnail sidecar, and `go test ./...` remains green.

### B18. Playlist Item Selection Parity (`--playlist-items`)
- Status: `[x]`
- Goal: Support deterministic partial playlist workflows using yt-dlp-style item selectors.
- Work:
  1. Add parser support for `--playlist-items`.
  2. Implement 1-based item/range selection (`N`, `N:M`, `:M`, `N:` and comma combinations).
  3. Apply filtering before flat-playlist emission and download processing.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Playlist flat and download flows process only selected items with stable ordering, invalid selectors fail early, and `go test ./...` remains green.

### B19. Playlist Range Aliases (`--playlist-start`, `--playlist-end`)
- Status: `[x]`
- Goal: Accept common yt-dlp playlist range aliases for simple partial playlist workflows.
- Work:
  1. Add parser support for `--playlist-start` and `--playlist-end`.
  2. Map aliases to the same 1-based playlist selection path as `--playlist-items`.
  3. Keep explicit `--playlist-items` as the more specific selector when both are present.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Start/end aliases filter flat and download playlist flows, invalid ranges fail early, and `go test ./...` remains green.

### B20. Playlist Reverse Ordering (`--playlist-reverse`)
- Status: `[x]`
- Goal: Support deterministic reverse playlist processing compatible with common yt-dlp usage.
- Work:
  1. Add parser support for `--playlist-reverse` and `--no-playlist-reverse`.
  2. Apply reverse ordering after item selection and before flat/download processing.
  3. Preserve default stable playlist ordering when the flag is absent.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Flat and download playlist flows process selected items in reverse order when requested, parser aliases are covered, and `go test ./...` remains green.

### B21. Playlist Failure Threshold (`--skip-playlist-after-errors`)
- Status: `[x]`
- Goal: Support yt-dlp-style playlist failure cutoff while preserving deterministic summary accounting.
- Work:
  1. Add parser support for `--skip-playlist-after-errors`.
  2. Stop playlist processing after N item failures, independently from `--abort-on-error`.
  3. Keep final summary totals explicit and tested.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Playlist processing stops after the configured failure threshold, disabled threshold values preserve default behavior, and `go test ./...` remains green.

### B22. Playlist Item Short Alias (`-I`)
- Status: `[x]`
- Goal: Accept yt-dlp's short alias for playlist item selection.
- Work:
  1. Add parser support for `-I` mapped to `--playlist-items`.
  2. Keep existing selector semantics and precedence unchanged.
  3. Document the alias.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-I 1:3` parses identically to `--playlist-items 1:3` and `go test ./...` remains green.

### B23. Playlist Item Hyphen Ranges (`START-STOP`)
- Status: `[x]`
- Goal: Support yt-dlp's backward-compatible positive hyphen range syntax for playlist item selection.
- Work:
  1. Extend playlist selector parsing to accept positive `START-STOP` ranges.
  2. Preserve existing comma composition and stable playlist order.
  3. Keep negative index/step syntax as a future parser expansion, not part of this narrow track.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--playlist-items 2-4` selects the same entries as `2:4`, invalid descending hyphen ranges fail, and `go test ./...` remains green.

### B24. Playlist Negative Indices (`--playlist-items -N`)
- Status: `[x]`
- Goal: Support yt-dlp-style negative playlist indices that count from the right.
- Work:
  1. Resolve single negative item selectors (`-1`) against the playlist length.
  2. Resolve negative bounds in colon ranges (`-3:-1`).
  3. Leave step syntax as a future parser expansion.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--playlist-items -1` selects the last item, `--playlist-items -3:-1` selects the final three items, out-of-range negative values produce an empty selection, and `go test ./...` remains green.

### B25. Playlist Positive Step Selectors (`START:STOP:STEP`)
- Status: `[x]`
- Goal: Support common yt-dlp playlist item step syntax for deterministic sampling workflows.
- Work:
  1. Extend colon range parsing from `START:STOP` to `START:STOP:STEP`.
  2. Support positive step values while preserving stable playlist order.
  3. Keep negative step semantics as a future reverse-order parser expansion.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--playlist-items 1:5:2` selects `1,3,5`, invalid/zero/negative step values fail clearly, and `go test ./...` remains green.

### B26. Playlist Negative Step Selectors (`START:STOP:-STEP`)
- Status: `[x]`
- Goal: Complete the practical yt-dlp playlist item range grammar by supporting reverse step selectors.
- Work:
  1. Preserve requested selector order instead of only original playlist order.
  2. Support negative step values such as `5:1:-2` and `::-1`.
  3. Keep duplicate suppression deterministic when selector segments overlap.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--playlist-items 5:1:-2` selects `5,3,1`, `--playlist-items ::-1` selects all items in reverse, overlapping selector segments do not duplicate items, and `go test ./...` remains green.

### B27. Playlist Random Ordering (`--playlist-random`)
- Status: `[x]`
- Goal: Support yt-dlp-style randomized playlist processing.
- Work:
  1. Add parser support for `--playlist-random`.
  2. Apply random ordering after item selection and after reverse precedence is considered.
  3. Keep tests deterministic via injectable random source helpers.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--playlist-random` shuffles selected items without dropping/duplicating entries, `--playlist-reverse` takes precedence when both are set, and `go test ./...` remains green.

### B28. Playlist Output Template Token (`%(playlist_index)s`)
- Status: `[x]`
- Goal: Reduce playlist filename collisions by supporting a common yt-dlp playlist template field.
- Work:
  1. Preserve original 1-based playlist index on playlist items.
  2. Render `%(playlist_index)s` in CLI output templates before per-item video processing.
  3. Keep package API additive and CLI logic adapter-only.
- Target files:
  - `client/types.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Playlist downloads and sidecars can use `%(playlist_index)s`, selected/reordered playlists retain original item indices, and `go test ./...` remains green.

### B29. Playlist Metadata Output Template Tokens
- Status: `[x]`
- Goal: Support common yt-dlp playlist metadata fields in per-item output templates.
- Work:
  1. Render `%(playlist_id)s` and `%(playlist_title)s` for playlist item processing.
  2. Preserve existing video token rendering and sidecar behavior.
  3. Keep token replacement in CLI adapter code.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Playlist downloads and sidecars can use playlist id/title tokens, values are filename-safe, and `go test ./...` remains green.

### B30. Playlist Count Output Template Token
- Status: `[x]`
- Goal: Support common yt-dlp playlist cardinality metadata in per-item output templates.
- Work:
  1. Render `%(playlist_count)s` for playlist item processing.
  2. Use the selected/reordered item set count so partial playlist workflows name outputs deterministically.
  3. Preserve existing playlist id/title/index token behavior.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Playlist downloads and sidecars can use a zero-padded playlist count token and `go test ./...` remains green.

### B31. Playlist Owner Output Template Tokens
- Status: `[x]`
- Goal: Align playlist owner metadata with yt-dlp's per-entry playlist context.
- Work:
  1. Extract playlist channel/uploader name, channel ID, and handle where present in YouTube playlist page metadata.
  2. Add fields to `client.PlaylistInfo` without breaking existing callers.
  3. Render `%(playlist_uploader)s`, `%(playlist_uploader_id)s`, `%(playlist_channel)s`, and `%(playlist_channel_id)s` in per-item output templates.
- Target files:
  - `client/types.go`
  - `client/playlist_transcript.go`
  - `client/playlist_transcript_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Playlist downloads and sidecars can include owner/channel template tokens, owner metadata is public on `PlaylistInfo`, and `go test ./...` remains green.

### B32. Archive Break-On-Existing Behavior
- Status: `[x]`
- Goal: Support yt-dlp-style early termination when a download archive hit is encountered.
- Work:
  1. Add parser support for `--break-on-existing` and `--no-break-on-existing`.
  2. Stop URL batch and playlist processing cleanly on archived video hits when enabled.
  3. Preserve default archive behavior as skip-and-continue.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Archive hits still skip by default, `--break-on-existing` stops remaining queued inputs without failure exit, and `go test ./...` remains green.

### B33. Force-Write Archive for Simulated Workflows
- Status: `[x]`
- Goal: Match yt-dlp's archive behavior for successful no-download metadata workflows.
- Work:
  1. Add parser support for `--force-write-archive`, `--force-write-download-archive`, and `--force-download-archive`.
  2. Record archive IDs after successful JSON/list/skip-download workflows when the force flag is set.
  3. Preserve default behavior of recording only completed downloads.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--force-write-archive --skip-download --download-archive FILE` records successful IDs, aliases parse, failures do not record, and `go test ./...` remains green.

### B34. Max Downloads Cutoff Behavior
- Status: `[x]`
- Goal: Support yt-dlp-style completed-download limits for batch and playlist workflows.
- Work:
  1. Add parser support for `--max-downloads`.
  2. Count completed downloads from the same path that records download archive entries.
  3. Stop remaining URL batch/playlist processing cleanly when the limit is reached.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--max-downloads N` stops after N successful downloads without treating the cutoff as a failure, skips/failures do not increment the counter, and `go test ./...` remains green.

### B35. No-Download-Archive Override Alias
- Status: `[x]`
- Goal: Match yt-dlp's explicit archive-disable flag for config/alias compatibility.
- Work:
  1. Add parser support for `--no-download-archive`.
  2. Preserve last-flag-wins behavior between `--download-archive FILE` and `--no-download-archive`.
  3. Keep runtime archive behavior unchanged when no archive path is active.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--download-archive FILE --no-download-archive` disables archive use, later `--download-archive FILE` re-enables it, and `go test ./...` remains green.

### B36. No-Download Alias
- Status: `[x]`
- Goal: Accept yt-dlp's common `--no-download` alias for no-download sidecar/metadata workflows.
- Work:
  1. Add parser support for `--no-download` mapped to `SkipDownload`.
  2. Preserve existing `--skip-download` behavior.
  3. Document the alias in the CLI feature list.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--no-download` parses identically to `--skip-download`, sidecar workflows continue to work, and `go test ./...` remains green.

### B37. Subtitle Compatibility Aliases
- Status: `[x]`
- Goal: Accept common yt-dlp subtitle aliases used in existing scripts.
- Work:
  1. Map `--write-automatic-subs` to `WriteAutoSubs`.
  2. Map `--srt-langs` to the existing subtitle language selector.
  3. Preserve existing `--write-auto-subs` and `--sub-lang/--sub-langs` behavior.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - New aliases parse identically to existing options and `go test ./...` remains green.

### B38. Negative Subtitle Compatibility Aliases
- Status: `[x]`
- Goal: Accept yt-dlp subtitle disable aliases and preserve command-line override ordering.
- Work:
  1. Add parser support for `--no-write-subs` and `--no-write-srt`.
  2. Add parser support for `--no-write-auto-subs` and `--no-write-automatic-subs`.
  3. Preserve last-flag-wins behavior for positive/negative subtitle write flags.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Negative aliases disable the matching subtitle write mode, later positive aliases re-enable it, `--write-srt` still forces SRT only when it is the active write-subtitles choice, and `go test ./...` remains green.

### B39. Subtitle Listing Workflow
- Status: `[x]`
- Goal: Support yt-dlp's common subtitle discovery command.
- Work:
  1. Add parser support for `--list-subs`.
  2. Print available subtitle tracks without downloading media.
  3. Preserve sidecar/download behavior when the flag is absent.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--list-subs` lists manual/automatic subtitle tracks and stops before downloads, parser and formatting are tested, and `go test ./...` remains green.

### B40. All Subtitles Workflow
- Status: `[x]`
- Goal: Support yt-dlp's common `--all-subs` subtitle language expansion.
- Work:
  1. Add parser support for `--all-subs`.
  2. Expand requested subtitle languages from available tracks before writing sidecars.
  3. Respect manual vs automatic subtitle selection according to existing `--write-subs` / `--write-auto-subs` intent.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--all-subs --write-subs` writes all manual subtitle languages, `--all-subs --write-auto-subs` can expand automatic subtitle languages, duplicate languages are suppressed deterministically, and `go test ./...` remains green.

### B41. Get Thumbnail URL Workflow
- Status: `[x]`
- Goal: Support yt-dlp's simple thumbnail URL discovery command.
- Work:
  1. Add parser support for `--get-thumbnail`.
  2. Print the selected thumbnail URL from extracted video metadata and stop before downloads.
  3. Preserve existing `--write-thumbnail` sidecar behavior.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--get-thumbnail` prints the selected thumbnail URL, errors clearly when unavailable, and `go test ./...` remains green.

### B42. No-Write-Thumbnail Override Alias
- Status: `[x]`
- Goal: Match yt-dlp's explicit thumbnail sidecar disable flag.
- Work:
  1. Add parser support for `--no-write-thumbnail`.
  2. Preserve last-flag-wins behavior between `--write-thumbnail` and `--no-write-thumbnail`.
  3. Keep `--get-thumbnail` URL printing independent from thumbnail sidecar writes.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--write-thumbnail --no-write-thumbnail` disables sidecar writing, later `--write-thumbnail` re-enables it, `--get-thumbnail` still works independently, and `go test ./...` remains green.

### B43. Simple Metadata Get Flags
- Status: `[x]`
- Goal: Support yt-dlp's simple metadata printing commands for scripts.
- Work:
  1. Add parser support for `-e`/`--get-title`, `--get-id`, `--get-description`, and `--get-duration`.
  2. Print requested fields from extracted metadata and stop before downloads.
  3. Preserve existing JSON/list/sidecar behavior when no get flags are present.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Simple get flags print deterministic metadata lines, duration is human-readable, combined flags preserve request order, and `go test ./...` remains green.

### B44. Get Selected Format Summary
- Status: `[x]`
- Goal: Support yt-dlp's `--get-format` script workflow for chosen format visibility.
- Work:
  1. Add parser support for `--get-format`.
  2. Select formats using the same practical `-f` selector path used by downloads.
  3. Print deterministic selected format summaries and stop before downloads.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--get-format` respects `-f` selector/numeric itag inputs, combined get flags preserve request order, selector failures return a clear error, and `go test ./...` remains green.

### B45. Get Filename Workflow
- Status: `[x]`
- Goal: Support yt-dlp's `--get-filename` script workflow without writing files.
- Work:
  1. Add parser support for `--get-filename`.
  2. Predict the same output path shape used by current ytv1 single/merged downloads.
  3. Respect existing `-o` template tokens and selected format/itag metadata.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--get-filename` respects `-o`, numeric itag and merged selector outputs, combined get flags preserve request order, and `go test ./...` remains green.

### B46. Get Direct Media URL Workflow
- Status: `[x]`
- Goal: Support yt-dlp's `--get-url` script workflow for selected direct media URLs without downloading.
- Work:
  1. Add parser support for `--get-url`.
  2. Select formats using the same practical `-f` selector path used by downloads.
  3. Resolve and print one direct URL per selected format, preserving combined get flag request order.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--get-url` respects numeric itag and merged selector outputs, combined get flags preserve request order, resolution failures return a clear error, and `go test ./...` remains green.

### B47. Simulate/No-Download Aliases
- Status: `[x]`
- Goal: Accept yt-dlp's common simulation aliases for no-download metadata and sidecar workflows.
- Work:
  1. Add parser support for `-s` and `--simulate` mapped to `SkipDownload`.
  2. Preserve existing `--skip-download` and `--no-download` behavior.
  3. Document the aliases.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-s` and `--simulate` parse identically to `--skip-download`, sidecar/get workflows continue to work, and `go test ./...` remains green.

### B48. Quiet Output Mode
- Status: `[x]`
- Goal: Accept yt-dlp's common quiet flag and suppress non-result human status output for automation workflows.
- Work:
  1. Add parser support for `-q` and `--quiet`.
  2. Suppress progress/status lines for download, skip-download, archive skips, playlist progress, and sidecar writes.
  3. Preserve intentional result outputs such as JSON, `--get-*`, `-F`, and `--list-subs`.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-q`/`--quiet` parse, quiet mode suppresses human status text without suppressing explicit result/list outputs, and `go test ./...` remains green.

### B49. Negative Simulate/Quiet Aliases
- Status: `[x]`
- Goal: Support yt-dlp-style opt-out aliases for common config-driven simulation and quiet modes.
- Work:
  1. Add parser support for `--no-simulate` mapped to `SkipDownload=false`.
  2. Add parser support for `--no-quiet` mapped to `Quiet=false`.
  3. Preserve last-flag-wins behavior for positive/negative simulate and quiet flags.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--simulate --no-simulate` downloads normally, `--quiet --no-quiet` restores human output, later positive flags re-enable each mode, and `go test ./...` remains green.

### B50. Print Field/Template Workflow
- Status: `[x]`
- Goal: Support yt-dlp's practical `-O`/`--print` automation workflow for video metadata outputs.
- Work:
  1. Add parser support for repeatable `-O`/`--print` arguments.
  2. Map simple field names to the existing get helpers (`title`, `id`, `description`, `duration`, `filename`, `format`, `url`, `thumbnail`).
  3. Render simple output-template tokens for `--print` values while preserving request order and implied quiet/simulate behavior.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--print title`, `-O url`, repeated print arguments, and basic `%(id)s/%(title)s/%(ext)s/%(duration)s` templates work without downloading, and `go test ./...` remains green.

### B51. Short Get-URL Alias
- Status: `[x]`
- Goal: Accept yt-dlp's common `-g` alias for direct media URL printing.
- Work:
  1. Add parser support for `-g` mapped to `GetURL`.
  2. Preserve combined get flag request ordering with `--get-url`.
  3. Document the alias.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-g` parses identically to `--get-url`, combined get flag order is preserved, and `go test ./...` remains green.

### B52. Print-To-File Workflow
- Status: `[x]`
- Goal: Support yt-dlp's `--print-to-file TEMPLATE FILE` automation workflow for video metadata outputs.
- Work:
  1. Add parser support for repeatable `--print-to-file TEMPLATE FILE` pairs.
  2. Reuse the B50 print template renderer and append one rendered line per request.
  3. Apply implied quiet/simulate behavior unless disabled later, consistent with `--print`.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--print-to-file title out.txt` appends rendered output without downloading, repeat usage is preserved, parent directories are created, and `go test ./...` remains green.

### B53. Subtitle Language `all` Selector
- Status: `[x]`
- Goal: Match yt-dlp's `--sub-langs all` subtitle language expansion behavior for common archive workflows.
- Work:
  1. Treat `all` in `--sub-lang/--sub-langs/--srt-langs` as a request to expand available subtitle tracks.
  2. Preserve manual/automatic subtitle intent from `--write-subs` and `--write-auto-subs`.
  3. Support simple exclusions such as `all,-live_chat`.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--write-subs --sub-langs all` expands manual tracks, `--write-auto-subs --sub-langs all,-live_chat` expands automatic tracks except excluded languages, and `go test ./...` remains green.

### B54. Explicit Subtitle Language Exclusions
- Status: `[x]`
- Goal: Match yt-dlp's negative subtitle language tokens for explicit language lists.
- Work:
  1. Filter `-language` tokens out of explicit `--sub-lang/--sub-langs/--srt-langs` requests.
  2. Exclude matching positive languages in the same request.
  3. Preserve deterministic duplicate suppression and fallback behavior.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--sub-langs en,ko,-ko` requests only `en`, `--sub-langs -ko` falls back consistently, and `go test ./...` remains green.

### B55. Convert Subtitles Format Aliases
- Status: `[x]`
- Goal: Accept yt-dlp's subtitle conversion aliases for scripts that request subtitle output formats.
- Work:
  1. Map `--convert-subs`, `--convert-sub`, and `--convert-subtitles` to the existing subtitle output format preference.
  2. Preserve `--sub-format` behavior and later-flag precedence where supported.
  3. Document the aliases.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--convert-subs srt` behaves like `--sub-format srt`, aliases parse, later subtitle format flags win, and `go test ./...` remains green.

### B56. Date Output Template Tokens
- Status: `[x]`
- Goal: Support common yt-dlp date fields in CLI output templates and print templates.
- Work:
  1. Render `%(upload_date)s`, `%(release_date)s`, and `%(timestamp)s` from extracted video metadata where available.
  2. Apply tokens consistently to `-o`, sidecar paths, `--print`, and `--print-to-file`.
  3. Preserve filesystem-safe normalization for filename contexts.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Date tokens render in predicted filenames and print templates, missing dates degrade to empty strings without panics, and `go test ./...` remains green.

### B57. Video Uploader/Channel Output Template Tokens
- Status: `[x]`
- Goal: Support common yt-dlp video owner/channel identifier fields in CLI templates.
- Work:
  1. Render `%(uploader_id)s`, `%(channel)s`, and `%(channel_id)s` from `VideoInfo`.
  2. Apply tokens consistently to `-o`, sidecar paths, `--print`, and `--print-to-file`.
  3. Keep filename-safe normalization in path contexts.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Owner/channel tokens render in predicted filenames and print templates, missing metadata renders empty strings, and `go test ./...` remains green.

### B58. Selected Format Output Template Tokens
- Status: `[x]`
- Goal: Support common yt-dlp selected-format fields in CLI templates.
- Work:
  1. Render `%(format_id)s` as an alias of selected itag information.
  2. Render `%(resolution)s`, `%(width)s`, `%(height)s`, and `%(fps)s` from the selected video format.
  3. Apply tokens to predicted filenames, sidecar paths, `--print`, and `--print-to-file`.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Selected-format tokens render for numeric and merged selections, audio-only selections degrade predictably, and `go test ./...` remains green.

### B59. Selected Format Bitrate Template Tokens
- Status: `[x]`
- Goal: Support common yt-dlp bitrate fields in CLI output templates.
- Work:
  1. Render `%(tbr)s` from selected total bitrate.
  2. Render `%(vbr)s` and `%(abr)s` from selected video/audio tracks where available.
  3. Apply tokens to predicted filenames, sidecar paths, `--print`, and `--print-to-file`.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Bitrate tokens render in kbit/s for numeric and merged selections, unavailable track-specific values render empty strings, and `go test ./...` remains green.

### B60. Selected Format Protocol Template Token
- Status: `[x]`
- Goal: Support yt-dlp's common `%(protocol)s` field in CLI output templates.
- Work:
  1. Render `%(protocol)s` from selected format protocol metadata.
  2. Join merged selection protocols deterministically.
  3. Apply the token to predicted filenames, sidecar paths, `--print`, and `--print-to-file`.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Protocol token renders for numeric and merged selections, missing protocol renders empty string, and `go test ./...` remains green.

### B61. Selected Format Codec Template Tokens
- Status: `[x]`
- Goal: Support common yt-dlp codec fields in CLI output templates.
- Work:
  1. Render `%(vcodec)s` and `%(acodec)s` from selected format MIME codec metadata where available.
  2. Use `none` for unavailable video/audio tracks, matching common yt-dlp expectations.
  3. Apply tokens to predicted filenames, sidecar paths, `--print`, and `--print-to-file`.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Codec tokens render for combined, audio-only, video-only, and merged selections, missing codec metadata degrades predictably, and `go test ./...` remains green.

### B62. Webpage/Original URL Print Fields
- Status: `[x]`
- Goal: Support common yt-dlp URL metadata fields in `--print` and `--print-to-file` templates.
- Work:
  1. Pass the original input URL into metadata print rendering.
  2. Render `webpage_url`/`original_url` field names and `%(webpage_url)s`/`%(original_url)s` template tokens.
  3. Preserve existing get flag behavior and output ordering.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--print webpage_url`, `--print original_url`, and matching template tokens print the input URL deterministically, and `go test ./...` remains green.

### B63. Batch File URL Input
- Status: `[x]`
- Goal: Support yt-dlp's `-a`/`--batch-file` automation workflow for URL lists.
- Work:
  1. Add parser support for `-a`/`--batch-file FILE` and `--no-batch-file`.
  2. Load URLs from the batch file, supporting `-` for stdin.
  3. Ignore blank lines and comment lines while preserving explicit CLI URL ordering after batch URLs.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Batch file URLs are processed before positional URLs, comments/blanks are skipped, `--no-batch-file` disables earlier batch input, and `go test ./...` remains green.

### B64. Output Paths Home Directory
- Status: `[x]`
- Goal: Support yt-dlp's practical `-P`/`--paths` workflow for directing downloads and sidecars to a base directory.
- Work:
  1. Add parser support for `-P PATH`, `--paths PATH`, and `--paths home:PATH`.
  2. Prefix relative output templates with the configured home path and provide a default template under that directory.
  3. Apply the effective output template to downloads, predicted filenames, subtitles, and sidecars.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-P out` places default downloads and sidecars under `out`, relative `-o` templates are rooted at `out`, absolute `-o` templates are unchanged, and `go test ./...` remains green.

### B65. ID Filename Shortcut
- Status: `[x]`
- Goal: Support yt-dlp's `--id` workflow for using the video ID as the output basename.
- Work:
  1. Add parser support for `--id` as an output-template shortcut.
  2. Apply `--id` as `%(id)s.%(ext)s` for downloads, predicted filenames, subtitles, and sidecars.
  3. Preserve practical precedence with `-o`/`--output` and compatibility with `-P`/`--paths`.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--id` predicts/downloads `VIDEO_ID.ext`, sidecars use `VIDEO_ID.*` basenames, `-P out --id` roots those paths under `out`, later explicit output templates can override the shortcut, and `go test ./...` remains green.

### B66. Restricted Filename Mode
- Status: `[x]`
- Goal: Support yt-dlp's `--restrict-filenames` workflow for ASCII-safe script-friendly output paths.
- Work:
  1. Add parser support for `--restrict-filenames` and `--no-restrict-filenames`.
  2. Apply restricted token sanitization to CLI output templates for downloads, predicted filenames, subtitles, and sidecars.
  3. Preserve default filename behavior unless restricted mode is explicitly requested.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--restrict-filenames -o "%(title)s.%(ext)s"` produces ASCII-safe names with spaces and non-portable characters normalized, `--no-restrict-filenames` can override it by order, sidecars follow the same basename policy, and `go test ./...` remains green.

### B67. Trim Filename Length Mode
- Status: `[x]`
- Goal: Support yt-dlp's `--trim-filenames LENGTH` workflow for avoiding overlong output basenames.
- Work:
  1. Add parser support for `--trim-filenames LENGTH`.
  2. Trim rendered filename basenames while preserving directory and extension suffixes.
  3. Apply trimming consistently to predicted filenames, downloads, subtitles, and sidecars.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--trim-filenames 8 -o "%(title)s.%(ext)s"` predicts and writes `TITLEPRE.ext`-style bounded basenames, sidecar suffixes remain intact where practical, invalid/non-positive lengths disable trimming without panics, and `go test ./...` remains green.

### B68. Concrete CLI Download Output Path
- Status: `[x]`
- Goal: Ensure actual CLI downloads use the same fully rendered path as `--get-filename`.
- Work:
  1. Build final `client.DownloadOptions` from extracted `VideoInfo` when downloading through the CLI adapter.
  2. Render advanced CLI template tokens, `--restrict-filenames`, and `--trim-filenames` before calling `client.Download`.
  3. Preserve package API behavior by keeping the concrete-path logic in `cmd/ytv1`.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - CLI download options receive the same concrete path predicted by `--get-filename`, restricted/trimmed filenames are applied to actual downloads, package-level template behavior is unchanged, and `go test ./...` remains green.

### B69. Overwrite Policy Flags
- Status: `[x]`
- Goal: Support yt-dlp-style overwrite controls for idempotent CLI runs.
- Work:
  1. Add parser support for `--no-overwrites` and `--force-overwrites`.
  2. Skip actual downloads when the final output path already exists under no-overwrite policy.
  3. Apply the same no-overwrite policy to metadata sidecars.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--no-overwrites` does not clobber existing media/sidecar files, `--force-overwrites` restores writes with later-flag precedence, skipped files report clean success, and `go test ./...` remains green.

### B70. Overwrite Alias and Resume Precedence
- Status: `[x]`
- Goal: Match yt-dlp overwrite flag aliases and force-overwrite interaction with resume behavior.
- Work:
  1. Add `-w`, `--yes-overwrites`, and `--no-force-overwrites` parsing.
  2. Preserve later-flag precedence among overwrite flags.
  3. Make final force-overwrite policy disable resume, matching yt-dlp.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-w` behaves like `--no-overwrites`, `--yes-overwrites` behaves like `--force-overwrites`, `--no-force-overwrites` restores default overwrite policy, final force-overwrite policy implies no-continue, and `go test ./...` remains green.

### B71. Trim Filename Alias
- Status: `[x]`
- Goal: Support yt-dlp's `--trim-file-names` alias for `--trim-filenames`.
- Work:
  1. Add parser support for `--trim-file-names LENGTH`.
  2. Preserve existing positive-length behavior and last-value precedence across both spellings.
  3. Document the alias.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--trim-file-names 8` behaves the same as `--trim-filenames 8`, both `--flag value` and `--flag=value` forms work, and `go test ./...` remains green.

### B72. Part File Download Workflow
- Status: `[x]`
- Goal: Support yt-dlp-style `--part` / `--no-part` controls for temporary media download files.
- Work:
  1. Add parser support for `--part` and `--no-part`.
  2. Add an additive package download option for temporary `.part` writes.
  3. Use `.part` files for CLI media downloads by default and rename on success; `--no-part` writes directly.
- Target files:
  - `client/download.go`
  - `client/download_test.go`
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - CLI default/`--part` uses `OUTPUT.part` during HTTP media download and renames to `OUTPUT` on success, `--no-part` keeps direct writes, resume can continue from an existing `.part`, and `go test ./...` remains green.

### B73. Media File Mtime Workflow
- Status: `[x]`
- Goal: Support yt-dlp-style `--mtime` / `--no-mtime` for setting downloaded media file modification time.
- Work:
  1. Add parser support for `--mtime` and `--no-mtime`.
  2. After successful CLI media downloads, set output file mtime from available YouTube upload/publish dates.
  3. Keep default behavior unchanged unless `--mtime` is explicitly requested.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--mtime` sets downloaded media file mtime from `UploadDate`/`PublishDate`, `--no-mtime` overrides it by order, missing/invalid dates are no-op without failure, and `go test ./...` remains green.

### B74. Internet Shortcut Sidecars
- Status: `[x]`
- Goal: Support yt-dlp's `--write-link` / `--write-url-link` workflow for saving a URL shortcut sidecar.
- Work:
  1. Add parser support for `--write-link` and `--write-url-link`.
  2. Write `.url` sidecars containing the original input URL using the existing output template basename policy.
  3. Respect no-overwrite policy and quiet/human output behavior.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--write-url-link` and practical `--write-link` write deterministic `.url` sidecars, output templates/path controls apply, existing files are skipped under `--no-overwrites`, and `go test ./...` remains green.

### B75. Webloc and Desktop Shortcut Sidecars
- Status: `[x]`
- Goal: Support yt-dlp's `--write-webloc-link` and `--write-desktop-link` shortcut sidecars.
- Work:
  1. Add parser support for `--write-webloc-link` and `--write-desktop-link`.
  2. Write deterministic `.webloc` and `.desktop` sidecars from the input URL.
  3. Reuse output template, path, restrict/trim, no-overwrite, and quiet behavior from existing sidecars.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--write-webloc-link` writes a valid plist `.webloc`, `--write-desktop-link` writes a deterministic Linux `.desktop` launcher, shared path policies apply, and `go test ./...` remains green.

### B76. Extract Audio Workflow
- Status: `[x]`
- Goal: Support common yt-dlp `-x` / `--extract-audio` and `--audio-format` workflows through existing audio download modes.
- Work:
  1. Add parser support for `-x`, `--extract-audio`, and `--audio-format FORMAT`.
  2. Map extract-audio with default/best format to audio-only mode.
  3. Map `--audio-format mp3` to existing MP3 transcode mode.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-x` and `--extract-audio` select audio-only downloads, `-x --audio-format mp3` selects MP3 mode, explicit `-f` still wins when provided, and `go test ./...` remains green.

### B77. Negative Metadata Sidecar Aliases
- Status: `[x]`
- Goal: Support yt-dlp's negative aliases for metadata sidecar writes.
- Work:
  1. Add parser support for `--no-write-info-json`.
  2. Add parser support for `--no-write-description`.
  3. Preserve last-flag-wins behavior against the positive sidecar flags.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--write-info-json --no-write-info-json` disables info JSON sidecars, later `--write-info-json` re-enables them, the same behavior applies to description sidecars, and `go test ./...` remains green.

### B78. Playlist Metadata Sidecar Controls
- Status: `[x]`
- Goal: Support yt-dlp-style playlist-level metadata sidecar behavior for common archive workflows.
- Work:
  1. Add parser support for `--write-playlist-metafiles` and `--no-write-playlist-metafiles`.
  2. Write a playlist `.info.json` sidecar when `--write-info-json` is used for playlist processing.
  3. Keep `--no-write-playlist-metafiles` scoped to playlist-level sidecars, not per-video sidecars.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Playlist processing with `--write-info-json` writes a deterministic playlist `.info.json` sidecar, `--no-write-playlist-metafiles` suppresses only that playlist-level file, and `go test ./...` remains green.

### B79. Audio Quality Transcode Option
- Status: `[x]`
- Goal: Preserve yt-dlp's `--audio-quality` intent for extract-audio MP3 workflows.
- Work:
  1. Add parser support for `--audio-quality QUALITY`.
  2. Carry audio quality through CLI download option construction.
  3. Expose the requested quality in MP3 transcode metadata for configured transcoders.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `client/download.go`
  - `client/download_mp3_test.go`
  - `client/transcoder.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--audio-quality` parses with last-value behavior, CLI MP3 extraction passes the quality to `client.DownloadOptions`, MP3 transcoders receive it in `MP3TranscodeMetadata`, and `go test ./...` remains green.

### B80. Embed Metadata Controls
- Status: `[x]`
- Goal: Support yt-dlp-style `--embed-metadata` / `--add-metadata` and negative aliases for merged output metadata.
- Work:
  1. Add parser support for `--embed-metadata`, `--add-metadata`, `--no-embed-metadata`, and `--no-add-metadata`.
  2. Keep CLI last-flag-wins behavior and default to no metadata embedding for yt-dlp parity.
  3. Add a client download option to suppress merge metadata without breaking existing library defaults.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `client/download.go`
  - `client/download_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - CLI defaults disable ffmpeg metadata embedding, positive aliases enable it, negative aliases disable it again by argument order, library defaults remain backward-compatible, and `go test ./...` remains green.

### B81. Postprocessor Args for FFmpeg Merge
- Status: `[x]`
- Goal: Support common yt-dlp `--postprocessor-args` / `--ppa` customization for ffmpeg merge commands.
- Work:
  1. Add repeatable parser support for `--postprocessor-args` and `--ppa`.
  2. Accept practical default/FFmpeg/Merger keys and split argument strings with shell-style quoting.
  3. Pass matching args into the ffmpeg muxer invocation for merged downloads.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `internal/muxer/ffmpeg.go`
  - `internal/muxer/ffmpeg_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Repeated `--ppa`/`--postprocessor-args` values are parsed in order, relevant default/FFmpeg/Merger args are included in ffmpeg merge commands, unsupported scoped keys are ignored conservatively, and `go test ./...` remains green.

### B82. Merge Output Format Option
- Status: `[x]`
- Goal: Support yt-dlp's `--merge-output-format FORMAT` for merged video+audio outputs.
- Work:
  1. Add parser support for `--merge-output-format FORMAT`.
  2. Carry the requested merge extension through CLI download option construction.
  3. Use the requested extension for predicted filenames and actual merged output paths.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `client/download.go`
  - `client/download_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--merge-output-format mkv` makes `--get-filename` and actual merged downloads end in `.mkv`, output templates receive `%(ext)s=mkv`, invalid/empty formats fall back conservatively, and `go test ./...` remains green.

### B83. Keep Intermediate Video Workflow
- Status: `[x]`
- Goal: Support yt-dlp's `-k` / `--keep-video` and `--no-keep-video` controls for post-processing intermediates.
- Work:
  1. Add parser support for `-k`, `--keep-video`, and `--no-keep-video`.
  2. Preserve last-flag-wins behavior.
  3. Map the setting to existing client intermediate-file retention during merged downloads.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-k` / `--keep-video` keep merged-download `.video`/`.audio` intermediates, `--no-keep-video` disables it by argument order, and `go test ./...` remains green.

### B84. Remux Video Container Option
- Status: `[x]`
- Goal: Support common yt-dlp `--remux-video FORMAT` workflows through existing ffmpeg merge output selection.
- Work:
  1. Add parser support for `--remux-video FORMAT`.
  2. Derive a practical output container from simple formats and slash-separated remux rules.
  3. Apply the derived container to predicted filenames and actual merged downloads when `--merge-output-format` is not more explicit.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--remux-video mkv` makes merged outputs end in `.mkv`, `--merge-output-format` takes precedence, simple remux rule strings choose a conservative target, and `go test ./...` remains green.

### B85. Post-Processed Overwrite Policy
- Status: `[x]`
- Goal: Support yt-dlp's `--post-overwrites` / `--no-post-overwrites` for post-processed outputs.
- Work:
  1. Add parser support for `--post-overwrites` and `--no-post-overwrites`.
  2. Preserve last-flag-wins behavior.
  3. Skip existing merged/MP3 post-processed outputs before starting download/post-processing when disabled.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--no-post-overwrites` skips existing merged or MP3 outputs, `--post-overwrites` re-enables overwrites by argument order, ordinary direct downloads keep the existing `--no-overwrites` policy, and `go test ./...` remains green.

### B86. Direct Download Rate Limiting
- Status: `[x]`
- Goal: Support common yt-dlp `-r` / `--limit-rate` / `--rate-limit` workflows for direct HTTP media downloads.
- Work:
  1. Add parser support for byte-rate values with common suffixes.
  2. Carry the rate limit into client download transport config.
  3. Apply the limiter to direct HTTP copy loops used by normal file downloads and MP3 source downloads.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `client/config.go`
  - `client/download.go`
  - `client/download_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-r 50K`, `--limit-rate 1M`, and `--rate-limit 100KiB` parse into bytes per second, direct HTTP downloads honor the configured limit in copy chunking/sleep behavior, invalid/empty values disable the limiter, and `go test ./...` remains green.

### B87. Download and Subtitle Sleep Intervals
- Status: `[x]`
- Goal: Support common yt-dlp sleep controls for download pacing.
- Work:
  1. Add parser support for `--sleep-interval`, `--min-sleep-interval`, `--max-sleep-interval`, and `--sleep-subtitles`.
  2. Sleep before media downloads, using a deterministic min value unless a higher max is provided.
  3. Sleep before subtitle file writes/downloads.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Sleep values parse as fractional seconds, media download flow invokes the configured delay before download, subtitle flow invokes `--sleep-subtitles`, invalid/empty values disable sleeps, and `go test ./...` remains green.

### B88. Extraction Request Sleep Interval
- Status: `[x]`
- Goal: Support yt-dlp's `--sleep-requests` pacing for extraction/list metadata requests.
- Work:
  1. Add parser support for `--sleep-requests SECONDS`.
  2. Sleep before video metadata extraction, playlist metadata extraction, and subtitle track listing requests.
  3. Keep subtitle transcript pacing under `--sleep-subtitles`.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--sleep-requests 0.75` parses as 750ms, extraction/list request paths invoke the configured delay, invalid/empty values disable request sleep, and `go test ./...` remains green.

### B89. Download Buffer and HTTP Chunk Sizing
- Status: `[x]`
- Goal: Support practical yt-dlp `--buffer-size` and `--http-chunk-size` controls for direct HTTP downloads.
- Work:
  1. Add parser support for `--buffer-size SIZE`, `--http-chunk-size SIZE`, `--resize-buffer`, and `--no-resize-buffer`.
  2. Parse common byte-size suffixes.
  3. Map the selected size to direct HTTP chunk size, with `--http-chunk-size` taking precedence.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--buffer-size 16K` and `--http-chunk-size 10M` parse into byte sizes, `--http-chunk-size` takes precedence in client config, resize flags are accepted for CLI compatibility, and `go test ./...` remains green.

### B90. Fragment Download Controls
- Status: `[x]`
- Goal: Support common yt-dlp fragment downloader flags for HLS/DASH workflows.
- Work:
  1. Add parser support for `-N` / `--concurrent-fragments`.
  2. Add parser support for `--skip-unavailable-fragments` / `--no-abort-on-unavailable-fragments`.
  3. Add parser support for `--abort-on-unavailable-fragments` / `--no-skip-unavailable-fragments`.
  4. Map the settings to existing HLS/DASH transport config.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `-N 8` controls fragment concurrency, unavailable-fragment positive/negative aliases use last-flag-wins behavior, settings reach `client.Config.DownloadTransport`, and `go test ./...` remains green.

### B91. Fragment Retry Count
- Status: `[x]`
- Goal: Support yt-dlp's `--fragment-retries RETRIES` for HLS/DASH fragment downloads.
- Work:
  1. Add parser support for `--fragment-retries`.
  2. Keep finite numeric retry counts deterministic.
  3. Map the value to fragment-capable download transport retry config, taking precedence over generic `--retries`.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--fragment-retries 7` reaches `client.Config.DownloadTransport.MaxRetries`, overrides generic `--retries` for fragment transport, invalid values are ignored, and `go test ./...` remains green.

### B92. Retry Sleep Expression Option
- Status: `[x]`
- Goal: Support yt-dlp's `--retry-sleep [TYPE:]EXPR` option for practical retry backoff configuration.
- Work:
  1. Add parser support for repeatable `--retry-sleep`.
  2. Support numeric, `linear=START[:END[:STEP]]`, and `exp=START[:END[:BASE]]` expressions by deriving the first retry sleep duration.
  3. Support `http`, `fragment`, and `extractor` type prefixes; ignore unsupported or invalid values.
  4. Map `http` and `fragment` to download transport backoff and `extractor` to metadata transport backoff.
  5. Document CLI behavior and add tests.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--retry-sleep 1.5` configures download retry initial backoff, `--retry-sleep fragment:2` overrides download/fragment backoff, `--retry-sleep extractor:3` configures metadata retry backoff, invalid values are ignored, and `go test ./...` remains green.

### B93. Socket Timeout Option
- Status: `[x]`
- Goal: Support yt-dlp's `--socket-timeout SECONDS` option for practical network timeout parity.
- Work:
  1. Add parser support for `--socket-timeout`.
  2. Map valid positive second values to `client.Config.RequestTimeout`.
  3. Ensure package request timeout can shorten a broader caller deadline.
  4. Document CLI behavior and add tests.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `client/request_helpers.go`
  - `client/request_helpers_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--socket-timeout 1.5` reaches `client.Config.RequestTimeout`, invalid values are ignored, request timeout can shorten an existing longer deadline, and `go test ./...` remains green.

### B94. Source Address Option
- Status: `[x]`
- Goal: Support yt-dlp's `--source-address IP`, `-4`, and `-6` network binding controls.
- Work:
  1. Add package config support for a source address used by the default HTTP client.
  2. Bind the default transport dialer to the configured local IP address.
  3. Add CLI parser support for `--source-address`, `-4` / `--force-ipv4`, and `-6` / `--force-ipv6` with last-flag-wins behavior.
  4. Document CLI behavior and add tests.
- Target files:
  - `client/config.go`
  - `client/client.go`
  - `client/http_client.go`
  - `client/http_client_test.go`
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--source-address 127.0.0.1` reaches `client.Config.SourceAddress`, `-4` maps to `0.0.0.0`, `-6` maps to `::`, the default HTTP transport binds valid local IPs, invalid source addresses fall back without panic, and `go test ./...` remains green.

### B95. Extractor Retry Count
- Status: `[x]`
- Goal: Support yt-dlp's `--extractor-retries RETRIES` option for metadata/extractor retry control.
- Work:
  1. Add parser support for `--extractor-retries`.
  2. Support finite nonnegative retry counts; ignore unsupported values for now.
  3. Map the value to metadata transport retry config, taking precedence over generic `--retries` for extractor requests.
  4. Document CLI behavior and add tests.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--extractor-retries 5` reaches `client.Config.MetadataTransport.MaxRetries`, overrides generic `--retries` for metadata transport only, invalid values are ignored, and `go test ./...` remains green.

### B96. Progress Output Controls
- Status: `[x]`
- Goal: Support yt-dlp's `--no-progress`, `--progress`, and `--newline` verbosity controls for practical CLI progress/status output parity.
- Work:
  1. Add parser support for `--no-progress`, `--progress`, and `--newline`.
  2. Preserve last-flag-wins behavior for progress enable/disable.
  3. Suppress download/playlist progress status text when `--no-progress` is active.
  4. Allow `--progress` to show progress status text even when quiet mode is active.
  5. Document CLI behavior and add tests.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--no-progress` suppresses progress/status output without suppressing explicit result/list outputs, `--progress` overrides quiet for progress/status output, `--newline` is accepted for compatibility, and `go test ./...` remains green.

### B97. No-Check-Certificates Option
- Status: `[x]`
- Goal: Support yt-dlp's `--no-check-certificates` option for environments with custom TLS interception or invalid server certificates.
- Work:
  1. Add package config support for disabling TLS certificate verification on the default HTTP client.
  2. Preserve explicit `HTTPClient` behavior when callers provide their own client.
  3. Add CLI parser support for `--no-check-certificates`.
  4. Document CLI behavior and add tests.
- Target files:
  - `client/config.go`
  - `client/client.go`
  - `client/http_client.go`
  - `client/http_client_test.go`
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--no-check-certificates` reaches client config, the default HTTP transport disables certificate verification only when requested, existing proxy/source-address behavior remains intact, and `go test ./...` remains green.

### B98. Request Header Controls
- Status: `[x]`
- Goal: Support yt-dlp's `--user-agent`, `--referer`, and `--add-headers FIELD:VALUE` request header controls.
- Work:
  1. Add CLI parser support for `--user-agent`.
  2. Add CLI parser support for `--referer`.
  3. Add repeatable parser support for `--add-headers FIELD:VALUE`.
  4. Map parsed headers to `client.Config.RequestHeaders` using existing request-header plumbing.
  5. Document CLI behavior and add tests.
- Target files:
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `--user-agent` maps to `User-Agent`, `--referer` maps to `Referer`, repeatable `--add-headers` maps custom headers with later values preserved by Go header semantics, invalid header specs are ignored, and `go test ./...` remains green.

### B99. Package-First Network Config Hardening
- Status: `[x]`
- Goal: Ensure yt-dlp-derived network behavior is exposed and verified as Go package functionality, not only CLI adapter behavior.
- Work:
  1. Add package-level tests proving `client.New(Config{...})` applies `ProxyURL`, `SourceAddress`, and `InsecureSkipVerify` to the generated HTTP client.
  2. Add package-level tests proving `RequestHeaders` flows through `ToInnerTubeConfig`.
  3. Correct `client.Config` documentation for request timeout and network knobs.
  4. Add README package usage examples for network/request-header configuration before CLI details.
- Target files:
  - `client/config.go`
  - `client/config_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package tests demonstrate direct Go API use of network configuration without going through CLI parsing, README shows package-first usage, and `go test ./...` remains green.

### B100. Package Throttled-Rate Detection
- Status: `[x]`
- Goal: Add package-level detection for sustained low-speed media downloads, inspired by yt-dlp's `--throttled-rate` behavior.
- Work:
  1. Add `client.DownloadTransportConfig` fields for throttled-rate threshold and minimum duration.
  2. Detect sustained low copy speed in direct HTTP media download paths.
  3. Treat throttling detection as retryable under existing download retry policy.
  4. Add package tests without relying on CLI parsing.
- Target files:
  - `client/config.go`
  - `client/download.go`
  - `client/download_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Direct package calls can configure throttled-rate detection through `client.Config.DownloadTransport`, sustained low speed triggers a retryable error, successful retry recovers, and `go test ./...` remains green.

### B101. Throttled Retry Writer Integrity
- Status: `[x]`
- Goal: Ensure package writer downloads do not expose partial throttled-attempt bytes after retry recovery.
- Work:
  1. Buffer each `downloadURLToWriterWithConfigAndHeaders` attempt internally.
  2. Commit bytes to the caller-provided writer only after an attempt succeeds.
  3. Add a package regression test for throttled first attempt followed by successful retry.
- Target files:
  - `client/download.go`
  - `client/download_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - A throttled first writer attempt followed by a successful retry returns only the successful retry body in the caller writer and `go test ./...` remains green.

### B102. Fragment Throttled-Rate Propagation
- Status: `[x]`
- Goal: Ensure package throttled-rate detection applies to HLS/DASH fragment fetches as well as direct HTTP media downloads.
- Work:
  1. Add throttled-rate fields to `internal/downloader.TransportConfig`.
  2. Detect sustained low-speed fragment/body reads as retryable throttling errors.
  3. Propagate `client.DownloadTransportConfig` throttled-rate settings into HLS/DASH downloader transport.
  4. Add internal/package tests.
- Target files:
  - `internal/downloader/transport.go`
  - `internal/downloader/transport_test.go`
  - `client/download.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - HLS/DASH downloader HTTP reads retry after sustained low speed when configured through package `DownloadTransportConfig`, and `go test ./...` remains green.

### B103. Resume Append Retry Integrity
- Status: `[x]`
- Goal: Ensure resumed range downloads do not duplicate partial bytes when a retryable append attempt fails.
- Work:
  1. Reset the output file to the original resume offset before retrying a failed range append attempt.
  2. Seek the append file back to the resume offset after truncation.
  3. Add package regression coverage for throttled first range response followed by successful retry.
- Target files:
  - `client/download.go`
  - `client/download_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - A resumed throttled first range response followed by successful retry produces exactly the original prefix plus successful retry bytes, without duplicated partial bytes, and `go test ./...` remains green.

### B104. Full Rewrite Retry Integrity
- Status: `[x]`
- Goal: Keep package file downloads streaming while preserving output integrity across full-rewrite retries.
- Work:
  1. Replace the full-rewrite file path's writer-buffer retry delegation with a file-specific streaming retry loop.
  2. Truncate and seek the destination file before each retryable full-rewrite attempt.
  3. Ensure throttled-rate settings are honored on full-rewrite file downloads.
  4. Add package regression coverage.
- Target files:
  - `client/download.go`
  - `client/download_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Full-rewrite file downloads retry throttled first attempts without retaining partial bytes, do not require buffering successful file bodies in memory, and `go test ./...` remains green.

### B105. Package File Access Retries
- Status: `[x]`
- Goal: Add package-level retry behavior for transient filesystem access failures, inspired by yt-dlp's `--file-access-retries`.
- Work:
  1. Add file access retry fields to `client.DownloadTransportConfig`.
  2. Add a small retry helper for filesystem operations.
  3. Apply it to `.part` file rename finalization.
  4. Add package tests using injectable file operation hooks.
- Target files:
  - `client/config.go`
  - `client/download.go`
  - `client/download_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can configure file access retry count/backoff, transient `.part` rename failures recover without corrupting output, and `go test ./...` remains green.

### B106. HLS/DASH Part Finalization Retries
- Status: `[x]`
- Goal: Apply package part-file finalization and file access retry behavior to HLS/DASH downloads, matching yt-dlp's fragment downloader use of temporary output plus retrying final rename.
- Work:
  1. Carry `DownloadOptions.UsePartFiles` into HLS/DASH package download paths.
  2. Write HLS/DASH output to `.part` when requested and finalize via the package file access retry helper.
  3. Add package tests for transient HLS/DASH final rename failures.
  4. Keep direct package configuration as the primary behavior surface.
- Target files:
  - `client/download.go`
  - `client/download_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - HLS/DASH package downloads honor part-file finalization, transient final rename failures recover under `DownloadTransportConfig.FileAccessRetries`, and `go test ./...` remains green.

### B107. Package File Open/Remove Retries
- Status: `[x]`
- Goal: Extend package-level file access retry behavior beyond final rename to transient download file open/create/remove failures, matching yt-dlp's `sanitize_open` and `try_remove` retry scope.
- Work:
  1. Add injectable file create/open/remove operations for package download tests.
  2. Apply `DownloadTransportConfig.FileAccessRetries` to package download output file creation/opening.
  3. Apply the same retry behavior to package intermediate-file cleanup.
  4. Add regression tests for transient create/remove failures.
- Target files:
  - `client/download.go`
  - `client/download_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can recover from transient output create/open/remove errors using `DownloadTransportConfig.FileAccessRetries`, and `go test ./...` remains green.

### B108. File Access Retry Defaults and CLI Adapter
- Status: `[x]`
- Goal: Match yt-dlp's default file access retry count at the package layer and expose `--file-access-retries` as a thin CLI adapter.
- Work:
  1. Normalize package file access retries to default to 3 attempts when unset.
  2. Allow negative package values to disable file access retry when callers need strict single-attempt behavior.
  3. Parse `--file-access-retries` and map it to `client.DownloadTransportConfig`.
  4. Add package and CLI config tests.
- Target files:
  - `client/download.go`
  - `client/download_test.go`
  - `internal/cli/parser.go`
  - `internal/cli/parser_test.go`
  - `README.md`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package downloads retry transient file access failures 3 times by default, CLI callers can override with `--file-access-retries`, and `go test ./...` remains green.

### B109. Thin CLI Refactor for Playlist Item Selection
- Status: `[x]`
- Goal: Move yt-dlp-style playlist item selector behavior from `cmd/ytv1/main.go` into the package layer so the CLI remains a thin adapter.
- Work:
  1. Add a package-level playlist item selection helper.
  2. Move selector range/index parsing out of `cmd/ytv1/main.go`.
  3. Update CLI workflow code to call the package helper.
  4. Keep existing selector behavior covered by tests.
- Target files:
  - `client/playlist_selection.go`
  - `client/playlist_selection_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `cmd/ytv1/main.go` no longer owns playlist item selector parsing, package callers can use the same selector behavior directly, and `go test ./...` remains green.

### B110. Thin CLI Refactor for Output Template Tokens
- Status: `[x]`
- Goal: Move output template token rendering and selected-format token calculation from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level output template data/rendering helpers.
  2. Add package-level selected-format token calculation helpers.
  3. Update CLI output prediction/print workflows to call package helpers.
  4. Preserve existing filename/template behavior with tests.
- Target files:
  - `client/output_template.go`
  - `client/output_template_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `cmd/ytv1/main.go` no longer owns generic output template token rendering or selected-format token calculation, package callers can use those helpers directly, and `go test ./...` remains green.

### B111. Thin CLI Refactor for Output Filename Prediction
- Status: `[x]`
- Goal: Move output filename prediction policy from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level output filename prediction options/API.
  2. Move merged/single-format filename prediction and trim policy out of CLI.
  3. Update CLI to pass selected formats and package options.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/output_filename.go`
  - `client/output_filename_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can predict output filenames directly, `cmd/ytv1/main.go` no longer owns core filename prediction policy, and `go test ./...` remains green.

### B112. Thin CLI Refactor for Sidecar Output Paths
- Status: `[x]`
- Goal: Move subtitle, metadata, description, shortcut, and thumbnail sidecar output path policy from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level sidecar output path options/API.
  2. Move sidecar path token rendering and extension policy out of CLI.
  3. Update CLI sidecar writers to call package helpers.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/sidecar_paths.go`
  - `client/sidecar_paths_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can compute sidecar output paths directly, `cmd/ytv1/main.go` no longer owns core sidecar path policy, and `go test ./...` remains green.

### B113. Thin CLI Refactor for Subtitle Language Selection
- Status: `[x]`
- Goal: Move yt-dlp-style subtitle language parsing, exclusions, and track expansion from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level subtitle language selector helpers.
  2. Move `all` and negative language exclusion behavior out of CLI.
  3. Update CLI subtitle workflow to call package helpers.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/subtitle_selection.go`
  - `client/subtitle_selection_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can reuse subtitle language selection directly, `cmd/ytv1/main.go` no longer owns core subtitle language policy, and `go test ./...` remains green.

### B114. Thin CLI Refactor for Download Archive
- Status: `[x]`
- Goal: Move download archive persistence and idempotency primitives from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add `client.DownloadArchive` with open/load/has/add/close behavior.
  2. Preserve corruption-tolerant archive loading and append-on-success semantics.
  3. Update CLI globals/helpers to use the package archive type.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/download_archive.go`
  - `client/download_archive_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `cmd/ytv1/workflow_matrix_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can use download archive idempotency directly, `cmd/ytv1/main.go` no longer owns archive persistence, and `go test ./...` remains green.

### B115. Thin CLI Refactor for Playlist Ordering
- Status: `[x]`
- Goal: Move playlist reverse/random ordering helpers from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level playlist ordering API.
  2. Preserve non-mutating reverse/random behavior.
  3. Update CLI helper to call package API.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/playlist_order.go`
  - `client/playlist_order_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can order playlist items directly, `cmd/ytv1/main.go` no longer owns ordering implementation, and `go test ./...` remains green.

### B116. Thin CLI Refactor for Shortcut Sidecar Body Rendering
- Status: `[x]`
- Goal: Move `.url`, `.webloc`, and `.desktop` sidecar body rendering from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level shortcut sidecar body renderer.
  2. Move XML and desktop-entry escaping out of CLI.
  3. Update CLI shortcut writer to call package API.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/sidecar_paths.go`
  - `client/sidecar_paths_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can render shortcut sidecar bodies directly, `cmd/ytv1/main.go` no longer owns shortcut body rendering, and `go test ./...` remains green.

### B117. Thin CLI Refactor for yt-dlp JSON Payload Builders
- Status: `[x]`
- Goal: Move yt-dlp-style single-video and playlist JSON payload construction from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level JSON payload structs/builders.
  2. Move canonical watch/playlist URL and direct format selection helpers out of CLI.
  3. Update CLI JSON emitters to encode package payloads.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/ytdlp_json.go`
  - `client/ytdlp_json_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can build yt-dlp-style JSON payloads directly, `cmd/ytv1/main.go` no longer owns payload construction, and `go test ./...` remains green.

### B118. Thin CLI Refactor for Selected Format Planning
- Status: `[x]`
- Goal: Move selected-format planning for yt-dlp-style selectors from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add a package-level selected-format planning API based on `DownloadOptions`.
  2. Share the selector defaulting/parsing path with package download behavior where possible.
  3. Update CLI `--get-format`, `--get-filename`, `--get-url`, and metadata helpers to call package API through a thin adapter.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/format_selection.go`
  - `client/format_selection_test.go`
  - `client/download.go`
  - `cmd/ytv1/main.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can preview selected formats directly from `DownloadOptions`, `cmd/ytv1/main.go` no longer owns selector parsing/selection, and `go test ./...` remains green.

### B119. Thin CLI Refactor for Format Summary Rendering
- Status: `[x]`
- Goal: Move selected-format summary strings and format-list note/ext rendering from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level helpers for media extension labels, track notes, and selected-format summaries.
  2. Update `-F` and `--get-format` CLI paths to call package helpers.
  3. Add package tests while preserving existing CLI output shape.
- Target files:
  - `client/format_summary.go`
  - `client/format_summary_test.go`
  - `cmd/ytv1/main.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can render selected format summaries directly, `cmd/ytv1/main.go` no longer owns track-note/ext summary logic, and `go test ./...` remains green.

### B120. Thin CLI Refactor for Metadata Print Rendering
- Status: `[x]`
- Goal: Move yt-dlp-style metadata print field and template rendering from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add package-level print rendering data/API for common fields and template tokens.
  2. Move duration formatting and print stage-prefix stripping out of CLI.
  3. Keep CLI responsible only for resolving dynamic filename/format/url values and file append I/O.
  4. Add package tests while preserving existing CLI output shape.
- Target files:
  - `client/metadata_print.go`
  - `client/metadata_print_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can render metadata print fields/templates directly, `cmd/ytv1/main.go` no longer owns field/token print rendering policy, and `go test ./...` remains green.

### B121. Thin CLI Refactor for Media MTime Policy
- Status: `[x]`
- Goal: Move media file modification-time derivation from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add a package helper that derives media mtime from upload/publish dates.
  2. Update CLI `--mtime` handling to call the package helper.
  3. Move date parsing tests to package coverage while preserving CLI file timestamp behavior.
- Target files:
  - `client/media_mtime.go`
  - `client/media_mtime_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can derive yt-dlp-style media mtime directly, CLI no longer owns date parsing policy, and `go test ./...` remains green.

### B122. Thin CLI Refactor for Thumbnail Download
- Status: `[x]`
- Goal: Move thumbnail download transport and file writing from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add a package API for downloading a video's selected thumbnail to a file path.
  2. Use the configured package HTTP client and validate HTTP status codes in package code.
  3. Update CLI thumbnail sidecar flow to handle path/overwrite/UI only.
  4. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/thumbnail_download.go`
  - `client/thumbnail_download_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can download thumbnail sidecars directly, `cmd/ytv1/main.go` no longer owns thumbnail HTTP transport/file copy logic, and `go test ./...` remains green.

### B123. Thin CLI Refactor for Description Sidecar Writing
- Status: `[x]`
- Goal: Move description sidecar file writing from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add a package API for writing a video's description to a sidecar file path.
  2. Keep CLI responsible only for path selection, overwrite policy, and user-facing status text.
  3. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/description_sidecar.go`
  - `client/description_sidecar_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can write description sidecars directly, `cmd/ytv1/main.go` no longer owns description directory/file write logic, and `go test ./...` remains green.

### B124. Thin CLI Refactor for Shortcut Sidecar Writing
- Status: `[x]`
- Goal: Move internet shortcut sidecar file writing from `cmd/ytv1/main.go` into the package layer.
- Work:
  1. Add a package API for writing `.url`, `.webloc`, and `.desktop` shortcut sidecars to a file path.
  2. Reuse package shortcut body rendering and keep CLI responsible only for path selection, overwrite policy, and user-facing status text.
  3. Add package tests while preserving existing CLI behavior.
- Target files:
  - `client/shortcut_sidecar.go`
  - `client/shortcut_sidecar_test.go`
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/main_test.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can write shortcut sidecars directly, `cmd/ytv1/main.go` no longer owns shortcut directory/file write logic, and `go test ./...` remains green.

### B125. Thin CLI Refactor for Reusable Download/Output Planning Helpers
- Status: `[x]`
- Goal: Move reusable download option construction, output-template composition, merge/remux extension policy, selected stream URL resolution, info JSON sidecar writing, and playlist metadata adaptation from `cmd/ytv1/main.go` into package APIs.
- Work:
  1. Add package APIs for common download-option alias mapping and output template directory/ID shortcut composition without depending on the CLI parser package.
  2. Add package APIs for selected stream URL resolution and merge/remux extension resolution.
  3. Add package APIs for video and playlist `.info.json` sidecar writing.
  4. Keep CLI responsible for parsing flags, overwrite checks, and user-facing output, while calling package helpers for reusable behavior.
- Target files:
  - `client/download_options.go`
  - `client/download_options_test.go`
  - `client/info_json_sidecar.go`
  - `client/info_json_sidecar_test.go`
  - `client/stream_urls.go`
  - `client/stream_urls_test.go`
  - `client/output_filename.go`
  - `client/output_filename_test.go`
  - `client/ytdlp_json.go`
  - `client/ytdlp_json_test.go`
  - `cmd/ytv1/main.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can build download options, compose output templates, resolve selected stream URLs, derive merge/remux extensions, write info JSON sidecars, and adapt playlist metadata without `cmd/ytv1`; `go test ./...` remains green.

### B126. Thin CLI Refactor for Subtitle, Playlist-Run, and List-Output Workflows
- Status: `[x]`
- Goal: Move reusable subtitle sidecar workflow, playlist item processing accounting, playlist template rendering, and list-output rendering from `cmd/ytv1/main.go` into package APIs.
- Work:
  1. Add package API for requested subtitle sidecar writes using transcript/subtitle-track APIs and shared sidecar path policy.
  2. Add package API for deterministic playlist item run summary/failure/skipped accounting and playlist template token rendering.
  3. Add package output writers for format lists, subtitle track lists, and flat playlist output.
- Target files:
  - `client/subtitle_sidecar.go`
  - `client/subtitle_sidecar_test.go`
  - `client/playlist_run.go`
  - `client/playlist_run_test.go`
  - `client/list_output.go`
  - `client/list_output_test.go`
  - `cmd/ytv1/main.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can run subtitle sidecar writes, process playlist item outcomes, render playlist template context, and emit list outputs without `cmd/ytv1`; `go test ./...` remains green.

### B127. Thin CLI Refactor for Metadata Print Workflow
- Status: `[x]`
- Goal: Move metadata/get/print rendering and print-to-file append behavior from `cmd/ytv1/main.go` into package APIs.
- Work:
  1. Add package request/result APIs for ordered metadata fields, print templates, selected filename/format/url rendering, and print-to-file path rendering.
  2. Add package file append helper for print-to-file outputs.
  3. Keep CLI responsible only for parser option mapping, stdout writes, and invoking package append behavior.
- Target files:
  - `client/metadata_workflow.go`
  - `client/metadata_print.go`
  - `client/metadata_print_test.go`
  - `cmd/ytv1/main.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can render ordered metadata print items and append print-to-file output without `cmd/ytv1`; `go test ./...` remains green.

### B128. Thin CLI Refactor for Diagnostics and Lifecycle Formatting
- Status: `[x]`
- Goal: Move diagnostics/remediation hint generation and lifecycle event formatting from `cmd/ytv1/main.go` into package APIs.
- Work:
  1. Add package helpers for remediation hints based on `AttemptDetail` and classified errors.
  2. Add package helpers for extraction/download event formatting and lifecycle timing accumulation.
  3. Keep CLI responsible only for choosing when to print diagnostics.
- Target files:
  - `client/diagnostics_format.go`
  - `cmd/ytv1/main.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - Package callers can format lifecycle events and derive remediation hints without `cmd/ytv1`; `go test ./...` remains green.

### B129. Thin `cmd/ytv1/main.go` Entrypoint Split
- Status: `[x]`
- Goal: Keep `cmd/ytv1/main.go` focused on process entry, startup, and URL batch dispatch after reusable behavior has moved into `client`.
- Work:
  1. Move video workflow orchestration into a command adapter file.
  2. Move remaining CLI-only helper adapters out of `main.go`.
  3. Keep `main.go` as the entrypoint/run-loop wrapper.
- Target files:
  - `cmd/ytv1/main.go`
  - `cmd/ytv1/video_workflow.go`
  - `cmd/ytv1/adapters.go`
  - `docs/IMPLEMENTATION_PLAN.md`
- Acceptance:
  - `cmd/ytv1/main.go` is substantially reduced and no longer owns extraction/download workflow bodies; `go test ./...` remains green.

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
- `2026-05-04`: Completed `B12` by removing human playlist stdout from JSON modes, enforcing last-flag-wins alias resolution for conflicting CLI flags, and routing startup/configuration failures through the structured JSON diagnostics path; verified with `go test ./...`.
- `2026-05-27`: Started `B13` after comparing latest local `D:\yt-dlp` YouTube changes for default clients, `web_creator`, `web_embedded`, and PO token cleanup.
- `2026-05-27`: Completed `B13` by aligning default clients with yt-dlp (`android_vr`, `web_safari`; authenticated `tv_downgraded`, `web_safari`), adding `web_creator`, changing embedded third-party context to a non-YouTube origin, sanitizing base64url PO tokens before player requests, and making audio-only downloads fall back to `best` when YouTube exposes no playable audio-only URL; verified with `go test ./...` and `YTV1_E2E=1 go test ./client -run TestE2E_ -count=1 -timeout 8m`.
- `2026-05-29`: Started `B14` to convert the current untracked player `base.js` into an explicit JS challenge drift regression, following yt-dlp's rule that invalid `n` solver output must not end with the original challenge.
- `2026-05-29`: Completed `B14` by adding a current `base.js` fixture regression, broadening runtime `n` URL export discovery to current obfuscated constructor names, rejecting invalid unchanged/sentinel `n` outputs in production code, and verifying with `go test ./...`.
- `2026-05-29`: Started `B15` for `--write-info-json` sidecar parity in common archive/skip-download metadata workflows.
- `2026-05-29`: Completed `B15` by adding `--write-info-json` parser support, deterministic `.info.json` template path derivation, sidecar emission from the yt-dlp-style metadata payload, README flag documentation, and parser/command tests; verified with `go test ./...`.
- `2026-05-29`: Started `B16` for `--write-description` sidecar parity in common metadata archival workflows.
- `2026-05-29`: Completed `B16` by adding `--write-description` parser support, deterministic `.description` template path derivation, sidecar file emission, README flag documentation, and parser/command tests; verified with `go test ./...`.
- `2026-05-29`: Started `B17` for thumbnail metadata exposure and `--write-thumbnail` sidecar parity.
- `2026-05-29`: Completed `B17` by adding public thumbnail fields to `client.VideoInfo`, selecting the highest-resolution thumbnail from Innertube metadata, adding `--write-thumbnail` parser/CLI sidecar download support, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B18` for `--playlist-items` partial playlist selection parity.
- `2026-05-29`: Completed `B18` by adding `--playlist-items` parser support, 1-based item/range filtering (`N`, `N:M`, `:M`, `N:` and comma combinations), applying selection before flat/download playlist processing, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B19` for `--playlist-start`/`--playlist-end` compatibility aliases.
- `2026-05-29`: Completed `B19` by adding `--playlist-start`/`--playlist-end` parser support, mapping aliases into the existing `--playlist-items` selector flow while letting explicit `--playlist-items` win, documenting the aliases, and verifying with `go test ./...`.
- `2026-05-29`: Started `B20` for deterministic `--playlist-reverse` ordering parity.
- `2026-05-29`: Completed `B20` by adding `--playlist-reverse`/`--no-playlist-reverse` parser support, applying reverse ordering after playlist item selection without mutating original slices, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B21` for `--skip-playlist-after-errors` playlist failure cutoff parity.
- `2026-05-29`: Completed `B21` by adding `--skip-playlist-after-errors` parser support, stopping playlist processing after N item failures independently from `--abort-on-error`, adding skipped-count summary accounting, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B22` for yt-dlp short playlist item alias `-I`.
- `2026-05-29`: Completed `B22` by mapping `-I` to `--playlist-items`, documenting the alias, adding parser regression coverage, and verifying with `go test ./...`.
- `2026-05-29`: Started `B23` for yt-dlp backward-compatible positive `START-STOP` playlist item ranges.
- `2026-05-29`: Completed `B23` by adding positive `START-STOP` parsing for `--playlist-items`, sharing range clamping with colon selectors, documenting the supported selector subset, and verifying with `go test ./...`.
- `2026-05-29`: Started `B24` for yt-dlp-style negative playlist item indices.
- `2026-05-29`: Completed `B24` by resolving negative single playlist selectors and negative colon-range bounds against playlist length, preserving positive hyphen range behavior, documenting the remaining step-syntax gap, and verifying with `go test ./...`.
- `2026-05-29`: Started `B25` for positive `START:STOP:STEP` playlist selector support.
- `2026-05-29`: Completed `B25` by extending colon-range parsing to positive step syntax, preserving stable playlist order, rejecting zero/negative step values explicitly, documenting the remaining negative-step gap, and verifying with `go test ./...`.
- `2026-05-29`: Started `B26` for negative playlist step selector support and requested-order preservation.
- `2026-05-29`: Completed `B26` by changing playlist selector filtering to preserve requested order, adding negative step support (`5:1:-2`, `::-1`), suppressing duplicate selected indices deterministically, updating docs, and verifying with `go test ./...`.
- `2026-05-29`: Started `B27` for yt-dlp-style `--playlist-random` ordering parity.
- `2026-05-29`: Completed `B27` by adding `--playlist-random` parser support, randomizing playlist order after item selection, preserving yt-dlp precedence where `--playlist-reverse` wins over random, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B28` for playlist `%(playlist_index)s` output template support.
- `2026-05-29`: Completed `B28` by adding additive `PlaylistIndex` metadata, filling missing original indexes before selection/reordering, rendering `%(playlist_index)s` and `%(playlist_autonumber)s` into per-item output templates, documenting the tokens, and verifying with `go test ./...`.
- `2026-05-29`: Started `B29` for playlist id/title output template token support.
- `2026-05-29`: Completed `B29` by passing playlist id/title context into per-item processing, rendering filename-safe `%(playlist_id)s` and `%(playlist_title)s` tokens alongside existing playlist index tokens, documenting the supported fields, and verifying with `go test ./...`.
- `2026-05-29`: Started `B30` for playlist count output template token support.
- `2026-05-29`: Completed `B30` by carrying selected playlist item count into per-item template rendering, adding zero-padded `%(playlist_count)s` support, documenting the token, covering it in playlist template tests, and verifying with `go test ./...`.
- `2026-05-29`: Started `B31` for playlist owner/channel metadata and output template token support.
- `2026-05-29`: Completed `B31` by adding public playlist owner/channel metadata fields, extracting them from YouTube playlist header/sidebar metadata, rendering `%(playlist_uploader)s`, `%(playlist_uploader_id)s`, `%(playlist_channel)s`, and `%(playlist_channel_id)s` in per-item output templates, documenting the tokens, and verifying with `go test ./...`.
- `2026-05-29`: Started `B32` for yt-dlp-style archive `--break-on-existing` behavior.
- `2026-05-29`: Completed `B32` by adding `--break-on-existing`/`--no-break-on-existing` parser support, stopping batch and playlist processing cleanly on archived video hits without converting the cutoff into a failure exit, preserving default skip-and-continue archive behavior, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B33` for yt-dlp-style `--force-write-archive` behavior in simulated metadata workflows.
- `2026-05-29`: Completed `B33` by adding force-write archive aliases, recording successful extracted IDs for JSON/list/skip-download no-download workflows only when requested, preserving default completed-download archive behavior, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B34` for yt-dlp-style `--max-downloads` cutoff behavior.
- `2026-05-29`: Completed `B34` by adding `--max-downloads` parser support, counting completed download records through the archive-record path, stopping URL batch and playlist processing cleanly when the limit is reached, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B35` for yt-dlp-style `--no-download-archive` override support.
- `2026-05-29`: Completed `B35` by adding `--no-download-archive`, reconciling it with `--download-archive` using last-flag-wins semantics including equals-form archive paths, documenting the alias, and verifying with `go test ./...`.
- `2026-05-29`: Started `B36` for yt-dlp-style `--no-download` alias support.
- `2026-05-29`: Completed `B36` by mapping `--no-download` to the existing `SkipDownload` option, documenting the alias, covering parser behavior, and verifying with `go test ./...`.
- `2026-05-29`: Started `B37` for yt-dlp subtitle alias compatibility.
- `2026-05-29`: Completed `B37` by mapping `--write-automatic-subs` to auto subtitle writes, mapping `--srt-langs` to the existing subtitle language selector, documenting the aliases, and verifying with `go test ./...`.
- `2026-05-29`: Started `B38` for yt-dlp negative subtitle alias compatibility.
- `2026-05-29`: Completed `B38` by accepting `--no-write-subs`, `--no-write-srt`, `--no-write-auto-subs`, and `--no-write-automatic-subs`, reconciling positive/negative subtitle write flags with last-flag-wins behavior, preserving `--write-srt` SRT forcing only when active, documenting the aliases, and verifying with `go test ./...`.
- `2026-05-29`: Started `B39` for yt-dlp-style `--list-subs` subtitle listing support.
- `2026-05-29`: Completed `B39` by adding `--list-subs` parser support, listing manual/automatic subtitle tracks through `client.GetSubtitleTracks`, stopping before media download like other listing flows, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B40` for yt-dlp-style `--all-subs` subtitle language expansion.
- `2026-05-29`: Completed `B40` by adding `--all-subs`, expanding requested subtitle languages from `client.GetSubtitleTracks` before sidecar writes, respecting manual vs automatic subtitle intent from `--write-subs` / `--write-auto-subs`, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B41` for yt-dlp-style `--get-thumbnail` thumbnail URL printing support.
- `2026-05-29`: Completed `B41` by adding `--get-thumbnail`, printing the selected thumbnail URL from extracted metadata, returning a typed unavailable error when no thumbnail exists, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B42` for yt-dlp-style `--no-write-thumbnail` override support.
- `2026-05-29`: Completed `B42` by adding `--no-write-thumbnail`, reconciling thumbnail sidecar write flags with last-flag-wins behavior, documenting the alias, and verifying with `go test ./...`.
- `2026-05-29`: Started `B43` for yt-dlp-style simple metadata `--get-*` printing flags.
- `2026-05-29`: Completed `B43` by adding `-e`/`--get-title`, `--get-id`, `--get-description`, and `--get-duration`, preserving request-order output for combined get flags, formatting durations as `M:SS`/`H:MM:SS`, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B44` for yt-dlp-style `--get-format` support.
- `2026-05-29`: Completed `B44` by adding `--get-format`, preserving combined get flag request order, selecting formats through the existing practical `-f` selector path including numeric itags, printing deterministic selected format summaries, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B45` for yt-dlp-style `--get-filename` support.
- `2026-05-29`: Completed `B45` by adding `--get-filename`, preserving combined get flag request order, predicting filenames through the same selected-format path as downloads, respecting `-o` template tokens for numeric and merged selections, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B46` for yt-dlp-style `--get-url` support.
- `2026-05-29`: Completed `B46` by adding `--get-url`, preserving combined get flag request order, selecting numeric/merged formats through the existing practical selector path, resolving ciphered stream URLs with `ResolveStreamURL`, printing direct non-ciphered URLs without extra resolution, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B47` for yt-dlp-style `-s`/`--simulate` no-download aliases.
- `2026-05-29`: Completed `B47` by mapping `-s` and `--simulate` to the existing `SkipDownload` workflow, documenting the aliases, extending parser regression coverage, and verifying with `go test ./...`.
- `2026-05-29`: Started `B48` for yt-dlp-style `-q`/`--quiet` output suppression.
- `2026-05-29`: Completed `B48` by adding `-q`/`--quiet`, suppressing download/skip/archive/playlist/sidecar human status lines and warnings, preserving explicit result outputs, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B49` for negative simulate/quiet aliases with last-flag-wins behavior.
- `2026-05-29`: Completed `B49` by adding `--no-simulate` and `--no-quiet`, reconciling positive/negative simulate and quiet flags with last-flag-wins behavior, documenting the aliases, and verifying with `go test ./...`.
- `2026-05-29`: Started `B50` for practical yt-dlp-style `-O`/`--print` field/template output.
- `2026-05-29`: Completed `B50` by adding repeatable `-O`/`--print`, preserving request order with existing get flags, supporting common field names plus simple output-template tokens, applying implied quiet/simulate unless disabled later, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B51` for yt-dlp-style short `-g` direct media URL alias.
- `2026-05-29`: Completed `B51` by mapping `-g` to `--get-url`, preserving combined get flag request ordering, documenting the alias, and verifying with `go test ./...`.
- `2026-05-29`: Started `B52` for practical yt-dlp-style `--print-to-file` output append support.
- `2026-05-29`: Completed `B52` by parsing repeatable `--print-to-file TEMPLATE FILE` pairs without leaking FILE into URL args, reusing the `--print` renderer, appending output lines with parent directory creation, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B53` for yt-dlp-style `--sub-langs all` subtitle language expansion.
- `2026-05-29`: Completed `B53` by treating `all` in subtitle language selectors as track expansion, preserving manual/automatic subtitle intent, supporting simple `-language` exclusions, documenting the behavior, and verifying with `go test ./...`.
- `2026-05-29`: Started `B54` for yt-dlp-style negative subtitle language tokens in explicit subtitle language lists.
- `2026-05-29`: Completed `B54` by filtering negative subtitle language tokens from explicit requests, excluding matching positive languages, preserving duplicate suppression/fallback behavior, documenting the syntax, and verifying with `go test ./...`.
- `2026-05-29`: Started `B55` for yt-dlp-style subtitle conversion format aliases.
- `2026-05-29`: Completed `B55` by mapping `--convert-subs`, `--convert-sub`, and `--convert-subtitles` to `SubFormat`, reconciling subtitle format aliases with later-flag precedence, documenting the aliases, and verifying with `go test ./...`.
- `2026-05-29`: Started `B56` for common yt-dlp date output template tokens.
- `2026-05-29`: Completed `B56` by adding `%(upload_date)s`, `%(release_date)s`, and `%(timestamp)s` to the CLI template renderer, applying them to predicted filenames, sidecar paths, `--print`, and `--print-to-file`, documenting the tokens, and verifying with `go test ./...`.
- `2026-05-29`: Started `B57` for video uploader/channel output template tokens.
- `2026-05-29`: Completed `B57` by adding `%(uploader_id)s`, `%(channel)s`, and `%(channel_id)s` to the CLI template renderer, applying them to predicted filenames, sidecar paths, `--print`, and `--print-to-file`, documenting the tokens, and verifying with `go test ./...`.
- `2026-05-29`: Started `B58` for selected-format output template tokens.
- `2026-05-29`: Completed `B58` by adding `%(format_id)s`, `%(resolution)s`, `%(width)s`, `%(height)s`, and `%(fps)s` to the CLI template renderer, filling values from selected single/merged formats, documenting the tokens, and verifying with `go test ./...`.
- `2026-05-29`: Started `B59` for selected-format bitrate output template tokens.
- `2026-05-29`: Completed `B59` by adding `%(tbr)s`, `%(vbr)s`, and `%(abr)s` to the CLI template renderer, deriving kbit/s values from selected single/merged formats, documenting the tokens, and verifying with `go test ./...`.
- `2026-05-29`: Started `B60` for selected-format protocol output template token.
- `2026-05-29`: Completed `B60` by adding `%(protocol)s` to the CLI template renderer, deriving values from selected single/merged formats with deterministic duplicate suppression, documenting the token, and verifying with `go test ./...`.
- `2026-05-29`: Started `B61` for selected-format codec output template tokens.
- `2026-05-29`: Completed `B61` by adding `%(vcodec)s` and `%(acodec)s` to the CLI template renderer, extracting codec values from selected format MIME metadata, using `none` for unavailable tracks, documenting the tokens, and verifying with `go test ./...`.
- `2026-05-29`: Started `B62` for webpage/original URL print template fields.
- `2026-05-29`: Completed `B62` by passing the input URL into metadata print rendering, adding `webpage_url`/`original_url` field names and `%(webpage_url)s`/`%(original_url)s` template tokens for print workflows, documenting the fields, and verifying with `go test ./...`.
- `2026-05-29`: Started `B63` for yt-dlp-style batch file URL input.
- `2026-05-29`: Completed `B63` by adding `-a`/`--batch-file`, loading URL lines before positional URLs, skipping blank/comment lines, supporting `-` for stdin, adding `--no-batch-file` override behavior, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B64` for yt-dlp-style `-P`/`--paths` home output directory support.
- `2026-05-29`: Completed `B64` by adding `-P`/`--paths` parsing, preserving Windows absolute paths, rooting relative/default output templates under the configured directory for downloads, predicted filenames, subtitles, and sidecars, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B65` for yt-dlp-style `--id` filename template shortcut support.
- `2026-05-29`: Completed `B65` by adding `--id` parsing with practical order precedence against `-o`/`--output`, applying the `%(id)s.%(ext)s` shortcut through the shared effective output template path, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B66` for yt-dlp-style `--restrict-filenames` ASCII-safe filename mode.
- `2026-05-29`: Completed `B66` by adding `--restrict-filenames` / `--no-restrict-filenames` parsing, applying restricted token normalization through CLI template rendering for downloads, predicted filenames, print-to-file paths, subtitles, and sidecars, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B67` for yt-dlp-style `--trim-filenames` basename length limiting.
- `2026-05-29`: Completed `B67` by adding `--trim-filenames` parsing, trimming rendered output basenames while preserving directories/extensions, covering predicted filenames and sidecars with regression tests, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B68` to route actual CLI downloads through the same concrete output path as `--get-filename`.
- `2026-05-29`: Completed `B68` by deriving concrete CLI download output paths from extracted `VideoInfo` before calling `client.Download`, keeping package-level template behavior unchanged, adding regression coverage for restricted/trimmed concrete paths, and verifying with `go test ./...`.
- `2026-05-29`: Started `B69` for yt-dlp-style overwrite policy flags.
- `2026-05-29`: Completed `B69` by adding `--no-overwrites` / `--force-overwrites` parsing with later-flag precedence, skipping existing concrete download and sidecar output paths under no-overwrite policy, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B70` for yt-dlp overwrite alias parity and force-overwrite resume policy.
- `2026-05-29`: Completed `B70` by adding `-w`, `--yes-overwrites`, and `--no-force-overwrites`, matching final overwrite-policy precedence, making final force-overwrite policy imply no-continue, documenting the aliases, and verifying with `go test ./...`.
- `2026-05-29`: Started `B71` for yt-dlp `--trim-file-names` alias support.
- `2026-05-29`: Completed `B71` by accepting `--trim-file-names` in both `--flag value` and `--flag=value` forms, sharing last-value precedence with `--trim-filenames`, documenting the alias, and verifying with `go test ./...`.
- `2026-05-29`: Started `B72` for yt-dlp-style `--part` / `--no-part` temporary download file workflow.
- `2026-05-29`: Completed `B72` by adding an additive `UsePartFiles` package download option, implementing HTTP media `.part` writes with final rename and resume from existing part files, parsing `--part` / `--no-part` with later-flag precedence, mapping CLI downloads to part files by default, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B73` for yt-dlp-style `--mtime` / `--no-mtime` media timestamp control.
- `2026-05-29`: Completed `B73` by adding `--mtime` / `--no-mtime` parsing with later-flag precedence, applying media output file modification time from upload/publish dates after successful CLI downloads, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B74` for yt-dlp-style `--write-link` / `--write-url-link` shortcut sidecars.
- `2026-05-29`: Completed `B74` by parsing `--write-link` / `--write-url-link`, writing `.url` sidecars from the input URL with shared output template/restrict/trim/path/no-overwrite behavior, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B75` for yt-dlp-style `--write-webloc-link` and `--write-desktop-link` shortcut sidecars.
- `2026-05-29`: Completed `B75` by parsing `--write-webloc-link` / `--write-desktop-link`, writing plist `.webloc` and Linux `.desktop` sidecars from the input URL with shared output template/restrict/trim/path/no-overwrite behavior, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B76` for yt-dlp-style `-x` / `--extract-audio` and `--audio-format` support.
- `2026-05-29`: Completed `B76` by parsing `-x` / `--extract-audio` and `--audio-format`, mapping default/best extraction to audio-only mode, mapping MP3 requests to existing MP3 mode, preserving explicit `-f` precedence, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B77` for yt-dlp-style `--no-write-info-json` and `--no-write-description` override support.
- `2026-05-29`: Completed `B77` by adding negative metadata sidecar parser aliases, reconciling them with positive flags by original argument order, documenting the aliases, and verifying with `go test ./...`.
- `2026-05-29`: Started `B78` for yt-dlp-style playlist metadata sidecar controls.
- `2026-05-29`: Completed `B78` by parsing `--write-playlist-metafiles` / `--no-write-playlist-metafiles`, writing playlist-level `.info.json` sidecars during playlist `--write-info-json` workflows, preserving per-video sidecars when playlist metafiles are disabled, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B79` for yt-dlp-style `--audio-quality` support in MP3 extraction workflows.
- `2026-05-29`: Completed `B79` by parsing `--audio-quality`, carrying it through CLI download option construction, exposing it in `client.MP3TranscodeMetadata`, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B80` for yt-dlp-style merged-output metadata embedding controls.
- `2026-05-29`: Completed `B80` by parsing embed metadata aliases with last-flag-wins behavior, defaulting CLI merges to no metadata embedding for yt-dlp parity, adding a backward-compatible client suppression option, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B81` for practical yt-dlp-style `--postprocessor-args` / `--ppa` support in ffmpeg merge workflows.
- `2026-05-29`: Completed `B81` by parsing repeated `--postprocessor-args` / `--ppa` values, applying practical default/FFmpeg/Merger scopes to the configured ffmpeg muxer, preserving unsupported scopes by ignoring them, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B82` for yt-dlp-style `--merge-output-format` support.
- `2026-05-29`: Completed `B82` by parsing `--merge-output-format`, carrying the normalized extension into client download options, applying it to CLI predicted filenames and actual merged output paths, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B83` for yt-dlp-style `-k` / `--keep-video` intermediate retention.
- `2026-05-29`: Completed `B83` by parsing `-k` / `--keep-video` / `--no-keep-video`, preserving last-flag-wins behavior, mapping the setting to client intermediate-file retention, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B84` for common yt-dlp-style `--remux-video` container selection.
- `2026-05-29`: Completed `B84` by parsing `--remux-video`, deriving practical target containers from simple values and remux rule strings, applying the result to predicted and actual merged output formats when `--merge-output-format` is absent, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B85` for yt-dlp-style post-processed output overwrite controls.
- `2026-05-29`: Completed `B85` by parsing `--post-overwrites` / `--no-post-overwrites`, preserving last-flag-wins behavior, skipping existing merged/MP3 post-processed outputs before work when disabled, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B86` for yt-dlp-style direct download rate limiting.
- `2026-05-29`: Completed `B86` by parsing `-r` / `--limit-rate` / `--rate-limit` byte-rate values, carrying them into download transport config, throttling direct HTTP copy paths and MP3 source reads, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B87` for yt-dlp-style sleep interval controls.
- `2026-05-29`: Completed `B87` by parsing fractional sleep interval flags, sleeping before media downloads, sleeping before subtitle transcript downloads, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B88` for yt-dlp-style `--sleep-requests` extraction request pacing.
- `2026-05-29`: Completed `B88` by parsing `--sleep-requests`, applying extraction-request sleeps before video metadata, playlist metadata, and subtitle-track list requests, documenting the flag, and verifying with `go test ./...`.
- `2026-05-29`: Started `B89` for yt-dlp-style direct download buffer and HTTP chunk sizing.
- `2026-05-29`: Completed `B89` by parsing `--buffer-size`, `--http-chunk-size`, `--resize-buffer`, and `--no-resize-buffer`, mapping explicit sizes into direct download transport chunk configuration with HTTP chunk size precedence, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B90` for yt-dlp-style fragment download controls.
- `2026-05-29`: Completed `B90` by parsing `-N` / `--concurrent-fragments` and unavailable-fragment skip/abort aliases, preserving last-flag-wins behavior, mapping settings into HLS/DASH download transport config, documenting the flags, and verifying with `go test ./...`.
- `2026-05-29`: Started `B91` for yt-dlp-style `--fragment-retries` support.
- `2026-05-29`: Completed `B91` by parsing finite nonnegative `--fragment-retries` values, ignoring invalid values, mapping them to download transport retry count with precedence over generic `--retries`, documenting the flag, and adding parser/config tests.
- `2026-05-29`: Started `B92` for yt-dlp-style `--retry-sleep [TYPE:]EXPR` support.
- `2026-05-29`: Completed `B92` by parsing repeatable numeric, `linear=...`, and `exp=...` retry sleep expressions, mapping default/http and fragment sleeps to download transport backoff, mapping extractor sleep to metadata transport backoff, ignoring unsupported values, documenting the flag, and adding tests.
- `2026-05-29`: Started `B93` for yt-dlp-style `--socket-timeout` support.
- `2026-05-29`: Completed `B93` by parsing positive second values for `--socket-timeout`, mapping them to `client.Config.RequestTimeout`, allowing request timeout to shorten broader caller deadlines, documenting the flag, and adding parser/config/helper tests.
- `2026-05-29`: Started `B94` for yt-dlp-style `--source-address`, `-4`, and `-6` support.
- `2026-05-29`: Completed `B94` by adding package source-address config, binding valid local IPs in the default HTTP transport dialer, parsing `--source-address`, `-4` / `--force-ipv4`, and `-6` / `--force-ipv6` with last-flag-wins behavior, documenting the flags, and adding CLI/client tests.
- `2026-05-29`: Started `B95` for yt-dlp-style `--extractor-retries` support.
- `2026-05-29`: Completed `B95` by parsing finite nonnegative `--extractor-retries` values, ignoring unsupported values, mapping them to metadata transport retry count with precedence over generic `--retries`, documenting the flag, and adding parser/config tests.
- `2026-05-29`: Started `B96` for yt-dlp-style progress output controls.
- `2026-05-29`: Completed `B96` by parsing `--no-progress`, `--progress`, and `--newline`, preserving last-flag-wins behavior for progress toggles, routing download and playlist progress/status text through a dedicated progress-output gate, documenting the flags, and adding parser/command tests.
- `2026-05-29`: Started `B97` for yt-dlp-style `--no-check-certificates` support.
- `2026-05-29`: Completed `B97` by adding package TLS verification override config, applying it only to generated default HTTP clients, parsing `--no-check-certificates`, documenting the flag, and adding CLI/client tests.
- `2026-05-29`: Started `B98` for yt-dlp-style request header controls.
- `2026-05-29`: Completed `B98` by parsing `--user-agent`, `--referer`, and repeatable `--add-headers FIELD:VALUE`, mapping valid values to `client.Config.RequestHeaders`, documenting the flags, and adding parser/config tests.
- `2026-05-29`: Started `B99` to refocus yt-dlp-derived network behavior as package-first `client.Config` functionality.
- `2026-05-29`: Completed `B99` by correcting package config timeout documentation, adding direct `client.New(Config{...})` tests for proxy/source-address/TLS network knobs, adding `ToInnerTubeConfig` request-header tests, and documenting package-first usage before CLI compatibility notes.
- `2026-05-29`: Started `B100` for package-first throttled-rate detection in direct media downloads.
- `2026-05-29`: Completed `B100` by adding `DownloadTransportConfig.ThrottledRateBytesPerSecond` and `ThrottledRateMinDuration`, detecting sustained low direct-download copy speed as a retryable throttling error, disabling chunked mode when throttled-rate detection is active, documenting package usage, and adding package retry tests.
- `2026-05-29`: Started `B101` to preserve writer output integrity across throttled-rate retries.
- `2026-05-29`: Completed `B101` by buffering each writer download attempt internally, committing only successful attempt bytes to the caller writer, and tightening the throttled-rate retry regression test to reject partial first-attempt bytes.
- `2026-05-29`: Started `B102` to propagate package throttled-rate detection into HLS/DASH fragment transport.
- `2026-05-29`: Completed `B102` by adding throttled-rate fields to internal fragment downloader transport, detecting sustained low-speed body reads as retryable throttling errors, propagating package `DownloadTransportConfig` settings into HLS/DASH transport, and adding internal downloader retry tests.
- `2026-05-29`: Started `B103` to preserve resumed range-append file integrity across retryable append failures.
- `2026-05-29`: Completed `B103` by truncating and seeking resumed range-append files back to the original resume offset before retry, and adding a throttled range retry regression test that rejects duplicated partial bytes.
- `2026-05-29`: Started `B104` to preserve full-rewrite file streaming and retry integrity under throttled-rate detection.
- `2026-05-29`: Completed `B104` by replacing full-rewrite file downloads' writer-buffer delegation with a file-specific streaming retry loop, truncating/seeking before each attempt, preserving throttled-rate detection, and adding a regression test for partial first-attempt cleanup.
- `2026-05-29`: Completed `B105` by adding package-level file access retry/backoff settings, applying them to `.part` rename finalization, documenting direct package usage, and covering transient rename recovery with a package test.
- `2026-05-29`: Completed `B106` by carrying package part-file finalization into HLS/DASH downloads, retrying transient final rename failures through `DownloadTransportConfig.FileAccessRetries`, and adding HLS/DASH package regression tests.
- `2026-05-29`: Completed `B107` by adding injectable package file create/open/remove operations, applying `DownloadTransportConfig.FileAccessRetries` to download file creation/opening and intermediate cleanup removal, and adding transient create/remove regression tests.
- `2026-05-29`: Completed `B108` by normalizing package file access retry defaults to 3 attempts, allowing negative package values to disable the default, parsing `--file-access-retries`, mapping it into `client.DownloadTransportConfig`, documenting the flag, and adding package/CLI tests.
- `2026-05-29`: Completed `B109` by adding `client.SelectPlaylistItems`, moving yt-dlp-style playlist selector parsing out of `cmd/ytv1/main.go`, updating the CLI to call the package API, and adding package regression tests.
- `2026-05-29`: Completed `B110` by adding package output template rendering and selected-format token helpers, updating CLI prediction/print workflows to call package helpers, and adding package regression tests.
- `2026-05-29`: Completed `B111` by adding `client.PredictOutputFilename` and related package filename helpers, reducing the CLI predicted filename path to selected-format plumbing, and adding package regression tests.
- `2026-05-29`: Completed `B112` by adding package sidecar path helpers for subtitles, info JSON, descriptions, shortcuts, and thumbnails, reducing CLI path helpers to package calls, and adding package regression tests.
- `2026-05-29`: Completed `B113` by adding package subtitle language parsing, `all` detection, exclusion filtering, and track expansion helpers, reducing CLI subtitle helpers to package calls, and adding package regression tests.
- `2026-05-29`: Completed `B114` by adding `client.DownloadArchive`, moving archive load/has/add/close behavior into the package layer, reducing CLI archive code to package calls, and adding package archive tests.
- `2026-05-29`: Completed `B115` by adding package playlist ordering helpers for original/reverse/random ordering, reducing CLI ordering wrappers to package calls, and adding package tests for non-mutating behavior.
- `2026-05-29`: Completed `B116` by adding package shortcut sidecar body rendering for `.url`, `.webloc`, and `.desktop`, moving XML/desktop escaping into the package layer, reducing CLI helper code to a package call, and adding package tests.
- `2026-05-29`: Completed `B117` by adding package yt-dlp-style JSON payload structs/builders, moving canonical URL and best direct-format helpers into the package layer, reducing CLI emitters to package payload encoding, and adding package tests.
- `2026-05-29`: Started `B118` to move selected-format planning out of `cmd/ytv1/main.go` and into package API.
- `2026-05-29`: Completed `B118` by adding `client.SelectFormatsForDownloadOptions`, sharing selector defaulting/parsing with package download behavior, reducing CLI selected-format planning to a package call, and verifying with `go test ./...`.
- `2026-05-29`: Started `B119` to move selected-format summary and list note rendering out of `cmd/ytv1/main.go`.
- `2026-05-29`: Completed `B119` by adding package format summary helpers, updating `-F` and `--get-format` paths to consume package rendering, moving the remaining track-note test coverage to `client.FormatTrackNote`, and verifying with `go test ./...`.
- `2026-05-29`: Started `B120` to move metadata print field/template rendering out of `cmd/ytv1/main.go`.
- `2026-05-29`: Completed `B120` by adding package metadata print rendering helpers, moving duration formatting and print stage-prefix stripping into `client`, reducing CLI print rendering to dynamic value assembly plus package rendering, and verifying with `go test ./...`.
- `2026-05-29`: Started `B121` to move media mtime date parsing policy out of `cmd/ytv1/main.go`.
- `2026-05-29`: Completed `B121` by adding `client.MediaFileMTime`, updating CLI `--mtime` handling to call package date derivation, moving date parsing coverage into package tests, and verifying with `go test ./...`.
- `2026-05-29`: Started `B122` to move thumbnail sidecar download transport out of `cmd/ytv1/main.go`.
- `2026-05-29`: Completed `B122` by adding `client.DownloadThumbnail`, moving thumbnail HTTP request/status handling and file writing into the package layer, reducing CLI thumbnail sidecar flow to path/overwrite/UI handling, and verifying with `go test ./...`.
- `2026-05-29`: Started `B123` to move description sidecar directory/file writing out of `cmd/ytv1/main.go`.
- `2026-05-29`: Completed `B123` by adding `client.WriteDescriptionSidecar`, moving description directory creation and file writing into the package layer, reducing CLI description sidecar flow to path/overwrite/UI handling, and verifying with `go test ./...`.
- `2026-05-29`: Started `B124` to move shortcut sidecar directory/file writing out of `cmd/ytv1/main.go`.
- `2026-05-29`: Completed `B124` by adding `client.WriteShortcutSidecar`, moving shortcut directory creation and file writing into the package layer for `.url`, `.webloc`, and `.desktop`, reducing CLI shortcut flow to path/overwrite/UI handling, and verifying with `go test ./...`.
- `2026-05-29`: Completed `B125` by adding package-level download option construction, output template composition, remux/merge extension resolution, selected stream URL resolution, info JSON sidecar writing, and playlist metadata adaptation helpers, reducing the CLI to parser-to-package mapping for those behaviors, and verifying with `go test ./...`.
- `2026-05-29`: Completed `B126` by adding package-level requested subtitle sidecar workflow, playlist item run accounting/template context helpers, and list-output writers for formats/subtitles/flat playlists, reducing the CLI to option mapping plus status text, and verifying with `go test ./...`.
- `2026-05-29`: Completed `B127` by adding package-level metadata print request/result rendering plus print-to-file path/append helpers, reducing CLI metadata output to option mapping and stdout/file dispatch, and verifying with `go test ./...`.
- `2026-05-29`: Completed `B128` by adding package-level diagnostics/remediation hint generation and lifecycle event formatting/timing helpers, reducing CLI diagnostics to print dispatch, and verifying with `go test ./...`.
- `2026-05-30`: Completed `B129` by moving the video workflow body to `cmd/ytv1/video_workflow.go` and remaining CLI adapter helpers to `cmd/ytv1/adapters.go`, reducing `cmd/ytv1/main.go` to the entrypoint/startup/run-loop layer and verifying with `go test ./...`.
- `2026-05-30`: Completed `B130` by trusting explicit codec metadata before progressive AV fallback, preventing video-only MP4 formats from being marked audio-capable, preserving codec-less progressive fallback behavior, verifying `oaevSXpWhdo` selection (`299+251`) and `-F` notes, and running `go test ./...`.
- `2026-05-30`: Completed `B131` by applying PR #9 security hardening to `dev`, fixing the PR's JS timeout cancellation bug, preserving long static DASH downloads by batching concurrent segment buffers instead of rejecting segment lists over 64 entries, restoring the root JSON `.gitignore` rule, adding targeted regression tests, and verifying with `go test ./...`.
- `2026-06-20`: Completed `B132` by removing the non-ciphered selection downgrade that could turn default high-quality video+audio downloads into low-quality plain streams, returning an explicit muxer-unavailable error instead of silently falling back when separate best video/audio formats require merging, preserving yt-dlp-style default output naming, validating live `8IY44RZHyw8` selection as `315+251`, and verifying with `go test ./...`.
- `2026-06-20`: Completed `B133` by moving the default unauthenticated client order to `ios, mweb, android_vr, web_safari` after reproducing MWEB 4K direct media `403` responses and validating that the default iOS-selected `315+251` URLs for `8IY44RZHyw8` return `206` to one-byte Range requests; verified with `go test ./...`.
- `2026-06-20`: Completed `B134` by comparing latest yt-dlp YouTube extractor commit `9ae7df9a22b29e2f81825230c9bba7d444190de0`, porting multi-client streaming format merge behavior into the Go orchestrator, preserving source-client metadata per raw format, replacing metadata-only duplicate itags with later playable URLs, changing default unauthenticated order to `android_vr, web_safari, ios, mweb`, and verifying `8IY44RZHyw8` default 4K `315+251` selection plus `206` Range responses and `go test ./...`.

---

## 7. Residual Risk Register (Post-Closeout)

1. `Medium` - Upstream YouTube behavior drift may break live extraction/download without code changes.
   owner: `internal/orchestrator`, `internal/playerjs`, `internal/challenge` maintainers.
2. `Low` - CLI substitute claim is YouTube-scoped; multi-site parity is intentionally out of scope.
   owner: product scope maintainers (`docs/IMPLEMENTATION_PLAN.md`, `README.md`).
3. `Low` - Live-gated confidence depends on periodic reruns; fixture matrix alone cannot detect all upstream runtime shifts.
   owner: release/checklist maintainers (`client/e2e_integration_test.go` + CI operators).
