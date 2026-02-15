# ytv1

Go-native YouTube extractor rewrite.

## Repo layout

- `legacy/kkdai-youtube`: original kkdai/youtube snapshot for reference
- `internal/*`: new extractor core
- `cmd/ytv1`: executable entrypoint

## Design goals

- No Python runtime dependency
- Fast adaptation to YouTube protocol changes
- Clear separation: client policy, player JS resolution, challenge solving, stream assembly
