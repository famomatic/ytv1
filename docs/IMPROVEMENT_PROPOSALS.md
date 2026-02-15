# Improvement Proposals for ytv1

These proposals outline logic and features that can improve upon `yt-dlp` and `kkdai/youtube` by leveraging Go's strengths (concurrency) and modern anti-fingerprinting techniques.

## 1. Concurrent "Racing" Extraction
**Current State**:
- `yt-dlp`: Sequential trial (`try android -> fail -> try web -> fail ...`). Slows down when YouTube blocks common clients.
- `kkdai`: Mostly single client or manual switching.

**Proposal**:
- **Logic**: Initiate extraction requests for `Android`, `iOS`, and `Web` clients *simultaneously* using Go goroutines.
- **Benefit**: Drastically reduce "Time to First Byte" (TTFB). The first successful response cancels the others.
- **Implementation**: `internal/orchestrator` launches 3 goroutines. First valid `VideoInfo` writes to a channel; others are cancelled via `context.WithCancel`.

## 2. TLS Fingerprinting (uTLS)
**Current State**:
- `yt-dlp`: Uses Python `ssl` (standard OpenSSL). Vulnerable to JA3 fingerprinting unless using `curl_cffi` plugin.
- `kkdai`: Uses Go `crypto/tls` (distinctive Go fingerprint, often blocked).

**Proposal**:
- **Logic**: Use `refraction-networking/utls` to byte-for-byte mimic the TLS handshake of the emulated client.
- **Detail**:
  - If behaving like `Android`: Send ClientHello consistent with OkHttp/Android.
  - If behaving like `Chrome`: Send ClientHello consistent with Chrome 120+.
- **Benefit**: Bypasses "403 Forbidden" errors caused by TLS fingerprint mismatch (a common YouTube blocker).

## 3. HTTP/2 Pseudo-Header Ordering
**Current State**:
- `net/http` (Go): Hardcodes pseudo-header order (`:method`, `:scheme`, etc.).
- `yt-dlp`: Requests often downgrade to HTTP/1.1 or use standard library order.

**Proposal**:
- **Logic**: Manually control header wire order to match the emulated browser/client exactly.
- **Benefit**: Defeats simple WAF rules that check for "Go-http-client" header anomalies.

## 4. Native Stream Splicing (Smart Reader)
**Current State**:
- `kkdai`: Downloads chunks to file.
- `yt-dlp`: Pipes to `ffmpeg` for merging video+audio.

**Proposal**:
- **Logic**: Implement a `io.ReadCloser` that virtually merged Video+Audio streams or HLS segments on the fly.
- **Benefit**: users can `io.Copy(player, client.GetStream())` and play 1080p (video+audio) directly without downloading to disk first or needing local `ffmpeg` for simple playback cases.

## 5. Centralized "Challenge Warehouse"
**Current State**:
- `yt-dlp`: Solves challenges (captcha/JS) reactively.

**Proposal**:
- **Logic**: A background worker that "pre-solves" or caches JS player interpretations.
- **Benefit**: If `tv` client needs `n` parameter solving, the solver might already have a "hot" engine ready, reducing latency.
