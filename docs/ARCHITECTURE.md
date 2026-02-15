# Architecture

## Core pipeline

1. Resolve candidate Innertube clients by policy.
2. Fetch player responses and pick usable streamingData.
3. Resolve player JS URL/variant.
4. Collect and solve signature/n challenges.
5. Emit playable stream URLs.

## References

- `legacy/kkdai-youtube`
- `d:/yt-dlp/yt_dlp/extractor/youtube`
