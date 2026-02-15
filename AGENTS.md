# AGENTS.md

## Project Mission

`ytv1` is a Go-native YouTube extraction library.

Primary goals:

1. Package-first architecture (library is the product, CLI is only an adapter).
2. Port critical extraction behavior from `yt-dlp` into idiomatic Go.
3. Keep external runtime dependencies minimal (no Python runtime dependency).
4. Make change-tracking sustainable via clear module boundaries and explicit plans.

## Functional Scope (v1)

Core package API:

1. `client.New(config)`
2. `client.GetVideo(ctx, input)`
3. `client.GetFormats(ctx, input)`
4. `client.ResolveStreamURL(ctx, videoID, itag)`

Core internal modules:

1. `internal/innertube`
2. `internal/policy`
3. `internal/orchestrator`
4. `internal/formats`
5. `internal/playerjs`
6. `internal/challenge`

## Source References

Use these as behavior references:

1. `legacy/kkdai-youtube`
2. `d:/yt-dlp/yt_dlp/extractor/youtube`

Port behavior, not code structure.

## Working Rules

1. Treat `docs/IMPLEMENTATION_PLAN.md` as the source of truth.
2. Always check `Current Snapshot` before coding.
3. Update plan status markers before/after substantial work.
4. Add new discovered tasks to `Immediate Next Tasks`.
5. Keep package API stable unless plan explicitly schedules an API break.
6. Keep CLI thin; do not move extraction logic into `cmd/ytv1`.
7. Keep `go test ./...` green after each logical change set.
8. Do not create or start tasks that are not in `docs/IMPLEMENTATION_PLAN.md` unless the user explicitly instructs it.
9. Do not skip ahead to new tasks while existing in-progress/pending task order is unresolved; finish or explicitly update status first.

## Progress Tracking Policy

When completing work:

1. Mark completed tasks as `[x]` in `docs/IMPLEMENTATION_PLAN.md`.
2. Mark active tasks as `[-]`.
3. Keep remaining tasks as `[ ]`.
4. If scope changes, update both this file and implementation plan.

## Quality Bar

Minimum acceptable for merge-ready progress:

1. Compiles and tests pass.
2. Public API behavior matches plan.
3. New behavior is covered by at least basic tests or explicit TODO with reason.
4. No hidden hardcoded runtime settings without config/fallback path.
