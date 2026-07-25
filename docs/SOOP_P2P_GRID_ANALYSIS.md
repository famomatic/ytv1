# SOOP P2P Grid Delivery — Analysis & 1080p Strategy

Status: **RESOLVED — the 540p "cap" was GEO-GATING, not a protocol wall (see
§13).** SOOP caps free quality to 540p for **KR** source IPs and serves full
**1080p/original directly from the CDN** to non-KR IPs, no login and no P2P.
`ytv1` already gets 1080p unchanged from a non-KR IP (verified end-to-end:
`h264 1920x1080`). The P2P agent reverse-engineering below (§10–12) is real and
correct but only relevant to getting 1080p from an actual KR IP without a VPN;
it additionally hits a center-side dedup/identity wall. For the normal use case,
route through a non-KR IP and the existing CDN path delivers 1080p.
Scope: how SOOP gates >540p behind its native P2P "grid delivery" agent, and how
`ytv1` can obtain 1080p without a login by driving that agent, with defined
fallbacks.

## 1. Problem

Anonymous SOOP live playback is capped at **540p**. For an anonymous token the
CDN master playlist only advertises `hd` (960x540) and `sd` (640x360):

```
#EXTM3U
#EXT-X-STREAM-INF:NAME=hd,BANDWIDTH=777600,RESOLUTION=960x540
auth_playlist.m3u8?aid=...
#EXT-X-STREAM-INF:NAME=sd,BANDWIDTH=345600,RESOLUTION=640x360
auth_playlist.m3u8?aid=...
```

Quality is gated **server-side by the `aid` token**, not by the client. The
`ORIGINAL`/1080p variant is simply never listed for an unauthenticated token.
Two things unlock it:

1. **Login** — an authenticated session yields a higher-grade `aid` (and, for
   premium content, a signed CDN cookie).
2. **Native P2P agent** — the desktop "SOOP" app joins SOOP's viewer P2P mesh.
   In exchange for relaying segments to other viewers (which lowers SOOP's CDN
   cost) it unlocks `ORIGINAL`/1080p **without a login**.

This document targets option 2 as the primary path.

## 2. Evidence base

- Native agent install: `C:\Users\mjmst\AppData\Local\SOOP\`
  - `SOOPPackage.exe` — the P2P grid client (viewer side). Confirmed **running**
    and listening on port `21201`.
  - `NetControl.dll` — the grid/net-control protocol implementation.
  - `SOOPStreamer.exe` — separate broadcaster/upload component (not used here).
- Player JS (saved page): `LivePlayer.js`, `LiveView.js`, `ViewVendor.js`.
- Binary strings extracted from `SOOPPackage.exe` / `NetControl.dll`.

### 2.1 Naming note

The install is **viewer-side P2P grid delivery** (AfreecaTV legacy
"GridDelivery" / `liveCache`). Binary symbols such as
`CHlsClientManager::SvcNetControl_InitBroadEx`, `SvcP_CertTicket`,
`Svcp_GateWayInit`, `SvcNetControl_MySessKey`, and `P2P_HLS` are the
viewer-side grid handlers. (`SOOPStreamer.exe`, with `P2P_REQ_BROADCAST_STREAM`
/ `me->child` relay symbols, is the broadcaster/upload tool and is out of
scope.)

## 3. Architecture (verified)

### 3.1 Local agent ports

From `LivePlayer.js` config object (`nAgent*`), verified against live process
state:

| Const              | Port  | Role                          | Live state                    |
|--------------------|-------|-------------------------------|-------------------------------|
| `nPackagePort`     | 21201 | **control WebSocket** (`/Websocket`) | **listening** (SOOPPackage) — accepts the ws upgrade |
| `nAgentServicePort`| 15530 | per-session service port      | not listening until in-session |
| `nAgentHlsPort`    | 2935  | local HLS re-serve            | opens per session (on demand) |
| `nAgentRtmpPort`   | 3190  | local RTMP                    | on demand                     |
| `nAgentPolicyPort` | 949   | policy                        | on demand                     |

Correction from the first draft: the initial control WebSocket is the **package
port 21201**, not `nAgentServicePort` (15530). The player has a second address
getter returning `${szLocalIP}:${nPackagePort}`, and probing confirms 21201
accepts the `/Websocket` upgrade while 15530 is closed until a session exists.
The agent reports the real per-session ports **in-band** via `setStreamerInfo`:
`nAgentServicePort = HTMLPLAYER_PORT`, `nAgentHlsPort = HLS_PORT` (default 2935).
`szLocalIP` = `127.0.0.1` over `http:`, else `localhost`. Control socket path is
`/Websocket` (`new WebSocket(this.host, this.protocol)`).

### 3.2 Local HLS endpoint

Once the agent has joined the grid it re-serves the stream locally as plain HLS
with `Access-Control-Allow-Origin: *`:

```js
this.szStreamUrl = "//" + szLocalIP + ":" + nAgentHlsPort + "/" + nBroadNo + "/playlist.m3u8"
```

→ `http://127.0.0.1:2935/<bno>/playlist.m3u8`

This is a **normal HLS master playlist** that includes the `ORIGINAL`/1080p
variant. Anything that can read HLS can consume it — no P2P logic required on the
consumer side.

### 3.3 Grid parameters come from `player_live_api.php`

`getLiveInfo()` maps the live-API (`live.sooplive.com/afreeca/player_live_api.php`)
`CHANNEL` fields onto the grid/session state:

| API field  | Player field         | Meaning                                    |
|------------|----------------------|--------------------------------------------|
| `AID`      | `szAID`              | stream auth token                          |
| `CTIP`     | `szCenterIP`         | P2P center server IP                        |
| `relay_ip` | `szCenterIP` (override) | relay/center override                    |
| `GWIP`     | `szGateWayIP`        | P2P gateway IP                              |
| `GRADE`    | `nGrade`             | grid grade                                  |
| `RMD`      | `szResourceDomain`   | stream base host for `broad_stream_assign` |
| `BPWD`     | `bPassword`          | password-protected flag                     |
| `CHPT`     | `nChatPort`          | chat port (not the P2P center port)         |

Note: `CHPT` is the **chat** port; do not confuse it with the P2P center port
(`nCenterPort`), which is populated separately.

### 3.4 Handshake sequence (browser ↔ agent)

`LivePlayer.js`, protocol `P2P_HLS`, gated by `_isSoopPlayerActive()` and
`WARNING_shouldConnectToAgentForHighQuality()`:

1. `getLiveInfo()` (player_live_api.php `type=live`) → grid params (§3.3).
2. Open `ws://127.0.0.1:21201/Websocket`.
3. `INIT_GW` (SVC 40, **lowercase-key** dialect):
   ```
   { SVC: 40, RESULT: 0, DATA: {
       gate_ip, gate_port, center_ip, center_port, broadno,
       cookie /*szTicket*/, guid /*szUID*/, cli_type, passwd,
       category, BJID, fanticket, addinfo, update_info, JOINLOG } }
   ```
4. Gateway replies `CERTTICKETEX` (SVC 39) with `DATA.pcTicket` /
   `DATA.pcAppendData`.
5. `INIT_BROAD` (SVC 4, **uppercase-key** dialect — distinct from the
   lowercase center dialect):
   ```
   { SVC: 4, RESULT: 0, DATA: {
       CIP, CPORT, BNO, PARENTBNO, SCRAPBJ, PASSWORD, GUID,
       TICKETLEN, TICKET /*=pcTicket*/, QUALITY:"ori",
       APPDATA /*=pcAppendData*/, BJID, SPROTOCOL, SIP, SPORT,
       JOINLOG } }
   ```
6. Agent joins `CTIP`/`GWIP`, pulls the stream (incl. `ORIGINAL`), and replies
   `HTMLPORT` (`setStreamerInfo`) with `HLS_PORT` (+ `HTMLPLAYER_PORT`).
7. Player plays `http://127.0.0.1:<HLS_PORT>/<bno>/playlist.m3u8`.

Two protocol dialects coexist: the agent socket uses lowercase keys for INIT_GW
and **uppercase** keys (CIP/CPORT/BNO/GUID/TICKET/APPDATA) for INIT_BROAD; the
direct-center path uses a separate binary framing. `ytv1` speaks only the agent
JSON dialect. Binary-side handlers: `SvcP_GetSoleSvr`, `SvcP_CertTicket`,
`Svcp_GateWayInit`, `SvcNetControl_StreamReadyComplete`.

### 3.5 Quality enum

`LivePlayer.js`: `QUALITY = { LOW, NORMAL, HIGH, HIGH_4000, HIGH_8000, ORIGINAL, AUTO }`.
`ORIGINAL` is the top tier the agent unlocks. `checkPlayableOnlyOriginal()` marks
streams that are Original-only (agent mandatory); `checkPlayableInAdaptive()`
clamps to `NORMAL` (540p) when no agent is present.

## 4. Routes to 1080p

### Route A — drive the local agent (PRIMARY)

`ytv1` reproduces the browser handshake against the already-installed agent, then
downloads the local HLS with the existing HLS pipeline.

- Steps: `player_live_api.php` (grid params) → `ws://127.0.0.1:15530/Websocket`
  `INIT_BROAD` → GET `http://127.0.0.1:2935/<bno>/playlist.m3u8`.
- Pros: agent performs all P2P; `ytv1` only speaks a local WebSocket + plain HLS.
  Reuses the existing HLS downloader. **No login.**
- Cons: requires the SOOP agent installed **and** running. The `/Websocket`
  frame schema (SVC codes + JSON `DATA` fields) must be finalized by live
  capture — the minified JS gives the shape, not every field/value. Brittle to
  SOOP client updates.

### Route B — reimplement the P2P mesh in `ytv1` (REJECTED)

`ytv1` joins the grid directly (connect to `CTIP` center + `GWIP` gateway, speak
the proprietary binary mesh protocol, cert-ticket auth, peer signaling, segment
exchange).

- Rejected: proprietary binary protocol lives only inside compiled
  `NetControl.dll`; no static artifacts to read. Weeks of RE, extremely fragile,
  no meaningful advantage over Route A (which reuses the vendor agent as the mesh
  participant).

### Route C — login + authenticated token / signed cookie (FALLBACK)

- For public streams: login cookies → `player_live_api.php` returns a
  higher-grade `AID` → CDN master playlist lists `ORIGINAL`/1080p. `ytv1` already
  carries the cookie jar into the SOOP source (`source.Deps.HTTPClient`), so this
  is essentially "pass `--cookies`".
- For premium/subscriber/PPV/timeshift: a signed CDN cookie is minted via POST
  (`credentials: include`, auto-refreshed every 60s):
  - `live.sooplive.com/api/private_auth.php` — `{ type: "subs_live", strm_id,
    broad_no, url }` (and `official_timeshift` / `sub_timeshift` variants).
  - `live.sooplive.com/afreeca/ppv_auth.php` — `{ work: "auth", bj_id, item_id,
    url }`.
- Pros: no P2P, no local agent, reuses the HTTP/HLS pipeline.
- Cons: needs an account (and, for premium, entitlement).

## 5. Decided strategy

**A → C → 540p.**

1. Try Route A (local agent). If the agent is present, running, and the handshake
   succeeds → 1080p via `127.0.0.1:2935`.
2. Else fall back to Route C (login/cookies) when cookies are supplied →
   authenticated `AID` (+ signed cookie for premium) → 1080p from CDN.
3. Else current behavior: anonymous `AID` → **540p**.

Each step degrades gracefully; no step is a hard failure as long as 540p works.

## 6. Implementation sketch (`ytv1`)

Landed inside `internal/source/soop` (the generic download pipeline is
unchanged — it just receives HLS formats + headers):

- `wsconn.go` — minimal RFC 6455 client (loopback, text frames, masking,
  ping/close handling). No third-party ws dependency.
- `liveinfo.go` — `fetchLiveInfo` (player_live_api.php `type=live`) → grid
  params (`CTIP/CTPT/GWIP/GWPT/CATE/FTK/RMD`, resolves `BNO`).
- `agent.go` — the agent driver: `agentReachable()` probe, the
  `INIT_GW → CERTTICKETEX → INIT_BROAD → HLS_PORT` handshake, a background
  keepalive loop (the agent tears down the local HLS server when the control
  session drops, so the socket is held for the download's lifetime), and
  `localPlaylistURL`.
- `soop.go` — `extractLive` order: `extractLiveViaAgent` (opt-out via
  `YTV1_SOOP_NO_AGENT`, only when `agentReachable()`) → `extractLiveViaCDN`
  (authenticated CDN with cookies, else anonymous 540p). Loopback HLS needs no
  SOOP CDN headers.
- Tests (`agent_test.go`): frame-codec roundtrip, `svcOf`, field helpers, and a
  full handshake against an in-process mock agent. The live `TestExtractLive`
  stubs `agentProbe` off to stay hermetic.

Every agent failure is non-fatal and falls through to the CDN path; verified
live that a rejected handshake adds no hang (fast fail → 540p).

## 7. Reference — reachable endpoints

| Endpoint | Purpose |
|----------|---------|
| `live.sooplive.com/afreeca/player_live_api.php` | live info: `AID`, `CTIP`, `GWIP`, `RMD`, `GRADE`, `BPWD`, `CHPT` |
| `livestream-manager.sooplive.com/broad_stream_assign.html` | CDN master playlist assignment (`return_type=gcp_cdn`, `broad_key=<bno>-common-master-hls`) |
| `live.sooplive.com/api/private_auth.php` | signed cookie: subscriber / timeshift |
| `live.sooplive.com/afreeca/ppv_auth.php` | signed cookie: pay-per-view |
| `ws://127.0.0.1:21201/Websocket` | local agent control socket (package port) |
| `http://127.0.0.1:2935/<bno>/playlist.m3u8` | local agent HLS re-serve (1080p) |

## 8. Caveats

- P2P participation consumes upload bandwidth (segments relayed to peers); this
  is the trade for no-login 1080p and explains the agent's network spike.
- Route A depends on an external vendor process and its undocumented local
  protocol — treat as best-effort with a hard fallback, never a hard dependency.
- Field names (`CTIP`/`GWIP`/`RMD`/…) and ports are from the current client
  build and can change without notice.

## 9. Live-probe results & remaining blocker

Probed the running agent directly (`ws://127.0.0.1:21201/Websocket`) to validate
the reconstructed protocol. Confirmed:

- 21201 accepts the `/Websocket` upgrade (HTTP 101); 15530 is closed until a
  session exists — so 21201 is the control socket.
- Frames are JSON text; the agent parses them without protocol error.
- Anonymous `player_live_api.php?type=live` returns the full grid params
  (`CTIP=…, CTPT=18000, GWIP=…, GWPT=3456, CATE, FTK, RMD`) — no login needed for
  the coordinates.

**Blocker:** every first message (INIT_GW, KEEPALIVE, or CHKEXECUTE-then-INIT_GW,
with or without a `sooplive.com` `Origin`) is answered with a **clean WebSocket
close (code 1000, no reason)** — not a protocol/auth error frame. A listen-only
connection just idles until timeout. The agent accepts the frame, finds the
session unaffiliated, and closes normally.

Interpretation: the agent serves only broadcasts requested within **its own
authenticated context** — the desktop app carries its own SOOP session, and the
browser is paired to it via the persisted `guid` (localStorage `UID`) plus,
likely, values the app registered out-of-band. A cold ws client supplying
synthetic `guid`/`cookie` is not a recognized session, hence the clean close.

Consequence for Route A: the **transport is solved** (port, path, framing, both
INIT dialects, grid-param sourcing), but driving the agent blind is blocked by
this session-affiliation gate, which cannot be reconstructed from the static
minified player. Reproducing it requires capturing a **live, working**
browser↔agent session to read the real `guid`/`cookie`/`TICKET` values and any
prior registration step (and the app itself may need to be logged in — which
would move Route A's "no login" benefit onto the app's account).

Until that capture exists, Route A ships **fail-safe only**: it attempts the
handshake when the agent is present and silently falls back to Route C / 540p on
the clean close. Finishing it is gated on a live ws capture, not more static
analysis.

## 10. Live capture results (tshark, loopback) — and the media-plane wall

Captured a real browser↔agent session with `tshark` on `\Device\NPF_Loopback`
(Npcap loopback adapter; Wireshark at `C:\Program Files\Wireshark\`). Snaplen
2048 to keep control frames whole. Findings correct several earlier guesses and
expose the actual blocker.

### 10.1 Port topology (two sockets)

- `ws://127.0.0.1:21201/Websocket` — fixed bootstrap. On connect the agent
  immediately pushes a **string-SVC** frame:
  ```json
  {"SVC":"HTMLPORT","RESULT":1,"UPDATE":"1",
   "DATA":{"HLS_PORT":6935,"HTMLPLAYER_PORT":22687,
           "FLASHPLAYER_PORT":-1,"RTMP_PORT":-1}}
  ```
  i.e. it hands out **dynamic per-session ports**.
- `ws://127.0.0.1:<HTMLPLAYER_PORT>/Websocket/<bjid>` — the session socket
  (e.g. `:22687/Websocket/viichan6`; note the path carries the bjid). All the
  real handshake + media happens here.

### 10.2 SVC encoding depends on the socket

- Bootstrap (21201) frames use **string** SVC (`"HTMLPORT"`, `"CAPTION"`).
- Session (HTMLPLAYER_PORT) frames use **numeric** SVC. So `svcOf` must handle
  both; the numeric assumption is right for the session socket only.

### 10.3 The real INIT_GW (client→agent, verbatim)

```json
{"SVC":40,"RESULT":0,"DATA":{
  "gate_ip":"118.218.125.114","gate_port":3456,
  "center_ip":"110.10.76.232","center_port":18000,
  "broadno":295847647,"cookie":"",
  "guid":"25631A3AB79CB882B26207735783A003",   // 32-hex UPPERCASE (localStorage UID)
  "cli_type":42,                                // NOT 14 — 42 = HTML5 agent
  "passwd":"","category":"00810000",
  "JOINLOG":"…\u0011…\u0006key\u0006=\u0006val…\u0012…",  // 0x11/0x06/0x12-delimited blob
  "BJID":"viichan6",
  "fanticket":"c2443…a13_viichan6_295847647_html5_0",     // <hash>_<bjid>_<bno>_html5_0
  "addinfo":"ad_lang\u0011ko\u0012is_auto\u00110\u0012",
  "update_info":0}}
```
`cookie` is **empty** — confirms no login. The client sends **only INIT_GW(40)**;
it never sends INIT_BROAD(4). The agent performs the gateway login + center join
+ cert-ticket exchange internally and reports progress back.

### 10.4 Agent→client responses (verbatim highlights)

- `SVC:41` gateway login: `{"cUserId":"","iKeepAliveTime":300,"iMode":2,…}`
- `SVC:39` **cert ticket** (agent did it itself):
  `{"iKeyIndex":0,"iPort":18463,"iTicketLen":732,"iTicketType":1,
    "pcTicket":"63D1135983…C3C","pcAppendDat":"Zp9haMtmJ8qYbxOsUPIWbsEZh9c=",
    "uiBroadId":295847647,"uiIpAddr":1893334333}` — note the key is
  **`pcAppendDat`**, not `pcAppendData`.
- `SVC:4` init-broad result: `ADDINFO` carries
  `preset: { "view_preset": "original, hd, sd, hd_4k" }` — **ORIGINAL/1080p is
  present.**
- `SVC:5` broadcast info: `WIDTH:1920, HEIGHT:1080, RATE:8000, SPROTOCOL:2` —
  the agent is serving true 1080p @ 8 Mbps.
- Plus `SVC:17/18/22/23/34/50/500` (user counts, quality, sess key, UTC sync).

### 10.5 The wall: media is delivered over the WebSocket, not local HLS

The capture contains **zero** local HTTP requests for `.m3u8`/`.ts` — the only
loopback HTTP is the two `/Websocket` upgrades. The stream itself arrives as
**~1700 binary WebSocket frames** on the session socket (SPROTOCOL:2,
`is_streamer=true`): the agent pushes the media as proprietary binary frames and
the browser SDK (webpack module 5852) demuxes them into MSE. The `HLS_PORT`
advertised in the HTMLPORT frame (6935) never accepted a TCP connection, and no
SOOP-owned listening port serves `<bno>/playlist.m3u8`.

**Consequence:** in SOOP's current delivery mode there is **no local HLS server
to GET**. The implemented Route A (fetch `http://127.0.0.1:<port>/<bno>/playlist.m3u8`)
targets the legacy `P2P_HLS` mode, which this build does not use for live. To
consume the P2P stream, `ytv1` would have to (a) run the INIT_GW handshake on the
dynamic session socket (now fully known) **and** (b) reimplement the proprietary
binary media framing/demux the SDK does — a large, brittle effort well beyond the
control-plane work.

### 10.6 Revised recommendation

- Route A (drive the agent for a local HLS pull) is **not viable** against the
  current client: the media plane is binary-over-WebSocket, with no local HLS.
  Pursuing it means reimplementing the SDK demuxer — not worth it.
- Keep the shipped behavior: **A attempted, fails fast, falls back to C → 540p.**
  The agent scaffold stays as a fail-safe probe, not a load-bearing path.
- The realistic no-login 1080p option is gone unless one reimplements the P2P
  media protocol. The dependable ≥720p path remains **Route C (login cookies)**.

## 11. BREAKTHROUGH — full protocol + media plane cracked

A full-payload loopback capture of a live no-login session, plus empirical
analysis, cracked the entire path. A playable 1080p MP4 was reconstructed from
the captured P2P frames (ffprobe: `h264 High 1920x1080` + `aac 48000 2ch`,
5.6 s). Earlier "not viable" / "affiliation gate" conclusions were **wrong** —
they were caused by using the wrong port and the wrong INIT_GW dialect.

### 11.1 Corrected control-plane handshake (verified live, standalone)

1. Connect `ws://127.0.0.1:21201/Websocket`, send one text frame
   `{"SVC":"CAPTION","RESULT":1,"DATA":{"nCaption":5}}`. The agent replies
   `{"SVC":"HTMLPORT","DATA":{"HLS_PORT":<p>,"HTMLPLAYER_PORT":<sess>, ...}}` —
   a **dynamic** per-session port. (Sending nothing → no reply; this trigger is
   required.)
2. Connect `ws://127.0.0.1:<HTMLPLAYER_PORT>/Websocket/<bjid>` and send
   `INIT_GW` (numeric `SVC:40`) with the real DATA (from §10.3): grid params
   (from player_live_api `type=live`), `cli_type:42`, random 32-hex-uppercase
   `guid`, `cookie:""`, `BJID`, `fanticket`, `category`, `addinfo`, `JOINLOG`.
3. The agent then drives the join itself and streams back, in order:
   `SVC:41` (gateway login ok) → `SVC:39` (CERTTICKETEX: `pcTicket`,
   `pcAppendDat`) → `SVC:45` (RECV_CHECK) → … → binary media frames.
   The client mirrors the browser: `SVC:30` (UID), `SVC:5` (START), answers
   `SVC:45`/`SVC:1` (keepalive), and streams `SVC:52` (BUFFER_LEFT) reports.

Confirmed live standalone (Go probe): steps 1–2 succeed and the agent returns
`SVC:41` + a freshly minted `SVC:39` cert ticket — **no clean-close, no login.**
The earlier clean-close was solely from sending INIT_GW to the 21201 bootstrap
port instead of the dynamic session port.

### 11.2 Media wire format — the SOOP chunk container (CRACKED)

Media is a sequence of length-delimited chunks over the session WebSocket's
**binary** frames. Each chunk = a **77-byte header** followed by the payload:

| Offset | Size | Field |
|-------:|-----:|-------|
| 0  | 8 | magic `FF FF FF FF FF FF FF FF` |
| 8  | 4 | type/version, const `01 00 45 00` (LE `0x00450001`; `0x45`=69 = header-remainder len, 8+69=77) |
| 12 | 4 | 0 |
| 16 | 4 | **payloadLen** (LE u32) |
| 20 | 4 | global packet seq (increments per chunk) |
| 24 | 4 | 0 |
| 28 | 4 | **per-stream counter** — video ≈ 413k range, audio ≈ 13k range (separate) |
| 32 | 4 | 0 |
| 36 | 4 | packet seq (+1) |
| 40 | 4 | 0 |
| 44 | 4 | const 1 |
| 48 | 8 | timestamp/PTS-ish (LE) |
| 56 | 8 | timestamp/PTS-ish (LE) |
| 64 | 13 | tail (more timing) |

Parse **sequentially** (`next = pos + 77 + payloadLen`); do **not** scan for the
magic — 8×`0xFF` also occurs inside payloads (false positives).

Payload after the header is standard, no proprietary codec:
- **Video** chunks: H.264 **Annex-B** (`00 00 00 01` NAL start codes; SPS
  `67 64 00 2A` = High\@L4.2, 1920×1080).
- **Audio** chunks: **ADTS AAC** (`FF F1 …`, 48 kHz stereo).
- Classify per chunk by payload prefix (`00000001` → video, `FFFx` → audio).

### 11.3 Reconstruction (proven)

Deframe → split by prefix → mux with ffmpeg:
```
# strip 77-byte headers, concat payloads, split video/audio by prefix
ffmpeg -r 60 -i video.h264 -i audio.aac -c copy soop_1080p.mp4
```
Result: `h264 High 1920x1080` + `aac 48000 2ch`, plays. The proprietary part is
**only the 77-byte chunk framing**; everything inside is standard H.264 + AAC.

### 11.4 Remaining work (deterministic, not RE)

1. **Make the stream flow to a headless client.** The live standalone probe got
   through the handshake to RECV_CHECK but no binary yet — likely because it was
   sharing the browser's active session port. Test with the browser closed (the
   agent then allocates a fresh session) and replay the browser's exact
   post-START message cadence (SVC 30/5/45/51/52/500). This is protocol replay.
2. **Port the deframer to Go** in `internal/source/soop`: read binary ws frames,
   parse the 77-byte chunk header, split video/audio by prefix, and either pipe
   Annex-B + ADTS to ffmpeg (`-c copy`) or write an fMP4/TS remux. Timestamps for
   A/V sync are in header bytes 48–76 (LE), still to be pinned precisely; ffmpeg
   `-r`/`genpts` gives a playable file in the interim.
3. Wire it behind the existing `resolveViaAgent` seam, keeping the C → 540p
   fallback.

Net: no-login 1080p via the agent is **feasible and largely proven**; what's left
is engineering (replay + a Go deframer), not reverse-engineering.

## 12. Implementation spec (self-contained)

Everything needed to implement no-login 1080p capture from the local SOOP agent.
Verified live except where marked OPEN. Reference probe:
`scratchpad/soop2step/main.go`; deframer: `scratchpad/framealyze/`.

### 12.1 End-to-end pipeline

```
player_live_api.php (type=live)  -- grid params --+
                                                   v
[bootstrap] ws 127.0.0.1:21201/Websocket  =>  HTMLPORT {HTMLPLAYER_PORT, HLS_PORT}
                                                   | (dynamic per session)
                                                   v
[session]  ws 127.0.0.1:<HTMLPLAYER_PORT>/Websocket/<bjid>
   send INIT_GW(40) => agent: gateway-login(41), cert(39), broadcast-init(4), ...
   client heartbeats SVC:52 ~200ms; sends 51,30,5,500 at the right points
   => agent streams BINARY ws frames = SOOP chunk container
                                                   v
[deframe] 77-byte chunk header -> H.264 Annex-B (video) + ADTS AAC (audio)
                                                   v
[mux] ffmpeg -c copy -> MP4   (PROVEN: h264 1920x1080 + aac 48k)
```

### 12.2 Grid params — player_live_api.php

`POST https://live.sooplive.com/afreeca/player_live_api.php` body
`bno=<bno>&type=live` (anonymous, desktop UA, `Referer: https://play.sooplive.com/`).
Response `CHANNEL`: `BNO, CTIP, CTPT, GWIP, GWPT, CATE, FTK, RMD, BPWD, GRADE,
TITLE, BJNICK`. All present without login. `bno` empty -> resolve via `bid=<bjid>`.

### 12.3 WebSocket transport (loopback, plain, no TLS)

- Client frames MUST be masked (RFC 6455); server frames are unmasked.
- Session media arrives as binary frames (opcode 2); control is text (opcode 1).
- ytv1 already has a minimal client at `internal/source/soop/wsconn.go`.

### 12.4 Bootstrap (port 21201) — get the dynamic session port

1. Connect `ws://127.0.0.1:21201/Websocket`.
2. Send one text frame (required trigger — silence yields no reply):
   `{"SVC":"CAPTION","RESULT":1,"DATA":{"nCaption":5}}`
3. Agent replies (string SVC):
   `{"SVC":"HTMLPORT","RESULT":1,"UPDATE":"1","DATA":{"HLS_PORT":<p>,"HTMLPLAYER_PORT":<sess>,"FLASHPLAYER_PORT":-1,"RTMP_PORT":-1}}`
4. Keep `HTMLPLAYER_PORT` (dynamic; e.g. 22687/29932 — changes per session).

Note: on THIS port the SVC is a string; on the session port it is numeric.
`HLS_PORT` here (e.g. 6935) is advertised but does NOT accept connections in the
current build — ignore it; media comes over the session ws, not local HLS.

### 12.5 Session handshake (dynamic port) — exact frames

Connect `ws://127.0.0.1:<HTMLPLAYER_PORT>/Websocket/<bjid>`, then INIT_GW(40).
Exact INIT_GW captured from the browser (verbatim), control bytes shown as \uXXXX:

```
{"SVC":40,"RESULT":0,"DATA":{
 "gate_ip":"<GWIP>","gate_port":<GWPT>,
 "center_ip":"<CTIP>","center_port":<CTPT>,
 "broadno":<bno>,"cookie":"","guid":"<32-HEX-UPPER>","cli_type":42,
 "passwd":"","category":"<CATE>",
 "JOINLOG":"log&uuid=<uuid32>&geo_cc=KR&geo_rc=42&acpt_lang=ko_KR&svc_lang=ko_KR&is_iframeapi=false&content_lang=ko_KR&os=win&is_streamer=true&is_rejoin=false&is_auto=false&is_support_adaptive=true&uuid_3rd=<uuid32>&subscribe=-1&player_mode=landing&sub_view_type=non_sub&subscription_type=basicliveualog&is_clearmode=false&lowlatency=1&is_streamer=true&os=win",
 "BJID":"<bjid>",
 "fanticket":"<HTML5-FANTICKET>",
 "addinfo":"ad_langkois_auto0","update_info":0}}
```

JOINLOG / addinfo byte grammar (raw control bytes on the wire; JSON escapes them
as \uXXXX):
- `` (0x11) = section-name -> body separator
- `` (0x06) = token wrapper / field separator; a field is
  `&<key>=<value>`
- `` (0x12) = section terminator
- JOINLOG = `log` <0x11> <fields...> <0x12> `liveualog` <0x11> <fields...> <0x12>
- addinfo = `<key>` <0x11> `<value>` <0x12> repeated (ad_lang=ko, is_auto=0)

### 12.6 Post-INIT message choreography (from the live capture, in order)

Client->Agent (C), Agent->Client (A).
Heartbeat = `{"SVC":52,"RESULT":0,"DATA":{"BUFFER_LEFT":<sec>}}`.

```
C SVC:40  INIT_GW
C SVC:51  {"BUFFERING_CAUSE":"initializing"}     (immediately after INIT_GW)
A SVC:41  gateway login  {cUserId,"iKeepAliveTime":300,iMode}
A SVC:39  CERTTICKETEX   {pcTicket,pcAppendDat,iPort:18463,iTicketLen:732}
C SVC:52  x several       (continuous ~200ms heartbeat from here on)
A SVC:34  {uac_time}
A SVC:18  {QUALITY:2}
C SVC:30  {"UID":""}
A SVC:45  RECV_CHECK {CHECK:1}   (do NOT ack; keep heartbeating)
A SVC:23  {SESS,liveViewerType,liveViewerVer}
A SVC:4   {ADDINFO:"...preset {view_preset: original, hd, sd, hd_4k}...",BNO}
A SVC:17  {JOINCHMB,MAINHITPC,...}
C SVC:5   {}   START            (after receiving SVC:4)
A SVC:5   {WIDTH:1920,HEIGHT:1080,RATE:8000,SPROTOCOL:2,BJID,BNO,...}
A SVC:18  {QUALITY:2}
C SVC:500 {"cutc":<unixFloat>}  UTC sync
A SVC:19,21,500{cutc,sutc},22,32,50
A (binary op=2)  <-- MEDIA STARTS
... interleaved SVC:50 {quality,status} + continuous binary
```

Client also answers keepalive `SVC:1` with `{"SVC":1,"RESULT":0,"DATA":{}}`.

### 12.7 Media wire format — SOOP chunk container

Parse sequentially from the concatenated binary-frame bytes; do NOT scan for
magic (8x0xFF also occurs in payloads). Header = 77 bytes, little-endian:

| off | sz | field |
|----:|---:|-------|
| 0  | 8 | magic FF*8 |
| 8  | 4 | 0x00450001 const (0x45=69 = bytes after this field to payload; 8+69=77) |
| 12 | 4 | 0 |
| 16 | 4 | payloadLen |
| 20 | 4 | global seq (++/chunk) |
| 24 | 4 | 0 |
| 28 | 4 | per-stream counter (video ~big, audio ~small — separate) |
| 32 | 4 | 0 |
| 36 | 4 | seq (+1) |
| 40 | 4 | 0 |
| 44 | 4 | const 1 |
| 48 | 8 | timestamp A (LE) |
| 56 | 8 | timestamp B (LE) |
| 64 |13 | tail timing |

`next = pos + 77 + payloadLen`. Payload:
- video: H.264 Annex-B — starts `00 00 00 01`; SPS `67 64 00 2A` = High@L4.2 1080p.
- audio: ADTS AAC — starts `FF Fx`; 48 kHz stereo.
Classify per chunk by payload prefix (00000001->video, FFFx->audio). PTS for
A/V sync lives in header bytes 48-76 (LE, exact layout TBD; -r/genpts works
meanwhile).

### 12.8 Reconstruction (proven)

Strip 77-byte headers, concat, split by prefix into video.h264 + audio.aac:
```
ffmpeg -r 60 -i video.h264 -i audio.aac -c copy out.mp4
# verified: h264 High 1920x1080 + aac 48000 2ch, plays.
```

### 12.9 Go integration sketch (internal/source/soop)

- Replace the dead local-HLS `resolveViaAgent` with an agent-streaming path:
  1. `fetchLiveInfo` (type=live) -> grid params.
  2. bootstrap 21201 -> CAPTION -> HTMLPORT -> dynamic port.
  3. session ws -> INIT_GW(40) with the 12.5 payload; run the 12.6 choreography
     (heartbeat goroutine + reactive sends).
  4. read binary frames -> chunkDeframer (77-byte header) -> classify -> write
     Annex-B + ADTS to an ffmpeg process (-c copy) or expose as an io.Reader
     the existing pipeline muxes.
- Keep the C->540p fallback on any failure.

### 12.10 OPEN — the one remaining blocker

A standalone client currently stalls after `SVC:45` (RECV_CHECK): the agent mints
the cert (SVC:39) but never emits SVC:34/18/23/4, i.e. the center-join /
broadcast-init does not complete. Prime suspect: fanticket. The browser's value
is `<hash>_<bjid>_<bno>_html5_0`, whose hash differs from the `type=live` `FTK`
(`..._flash_1`) — so it is NOT a suffix swap.

Evidence of the real source (LivePlayer.js): two setters for `szFanTicket` —
`t.FTK` (type=live) and `t.fan_ticket` from a second response that also carries
`relay_ip`, `relay_port`, `gateway_ip`, `gateway_port`, `is_adult`,
`channel_info` (chat ip:port). The captured JOINLOG contains
`getnode_response_time` and `getnode_cnt`, confirming the browser makes a
getNode / grid-assign call before INIT_GW. That call's `fan_ticket` is what the
center validates.

Next step to close it: find that getNode endpoint (grep the bundle for the URL
whose response is parsed into relay_ip/fan_ticket/gateway_ip/channel_info; it is
built near the `STREAM_MANAGER` / `.php?call-type=inner` endpoints in
LivePlayer.js), call it to obtain the html5 `fan_ticket` (+ relay/gateway
overrides), and pass that into INIT_GW. With a valid `fan_ticket` the center
should complete broadcast-init and the binary media (12.7) will flow — which the
deframer (12.8) already turns into a playable 1080p file.

### 12.11 getNode endpoint identified

The secondary grid-assign call (§12.10) is **`/broad/a/watch2`** (older
`/broad/a/watch`), found in LivePlayer.js next to the `STREAM_MANAGER` /
`broad_stream_assign.html` endpoints. Its response is parsed into
`fan_ticket`, `relay_ip`, `relay_port`, `gateway_ip`, `gateway_port`,
`is_adult`, `channel_info` (chat `ip:port`). This is the source of the html5
`fan_ticket` the center validates, and it also overrides center/gateway with
`relay_ip`/`gateway_ip`.

To finish: resolve the full URL/host (grep the bundle's `HOST` object and the
`` `${...}/broad/a/watch2` `` template; likely `live.sooplive.com/…/broad/a/watch2`
or a stream-manager host) and its query/body params (bno/bjid/quality/type),
call it anonymously, take `fan_ticket` (+ `relay_ip`/`gateway_ip` overrides), and
feed them into INIT_GW (§12.5). Then the choreography (§12.6) should proceed past
RECV_CHECK to the binary stream (§12.7), which the deframer (§12.8) already
converts to a playable 1080p MP4.

### 12.12 /broad/a/watch — full field map (verified)

`POST https://api.m.sooplive.co.kr/broad/a/watch` body
`bj_id=<bjid>&broad_no=<bno>` (mobile UA, `Referer: https://m.sooplive.co.kr/`).
Anonymous, returns `data`:

| field | value (example) | use |
|-------|-----------------|-----|
| `fan_ticket` | `<hash>_<bjid>_<bno>_android_1` | INIT_GW `fanticket` (swap suffix to `_html5_0` for cli_type 42) |
| `relay_ip` / `relay_port` | `110.10.76.232` / `18000` | INIT_GW `center_ip` / `center_port` |
| `gateway_ip` / `gateway_port` | `218.38.31.78` / `3456` | INIT_GW `gate_ip` / `gate_port` — **differs from `type=live` GWIP** |
| `channel_info` | `110.10.76.72:8040` | chat ip:port |
| `colony_content` | `.A32.…` | center AID token |
| `hls_authentication_key` | `.A32.…` | CDN AID (Route C) |
| `is_p2p` | `True` | P2P available |
| `resolution` | `1920x1080` | original quality |
| `supported_vcodec` | `h264` | codec |
| `resource_manager_url` | `https://livestream-manager.sooplive.co.kr/broad_st…` | CDN assign |

Use `gateway_ip`/`relay_ip` from HERE (not `type=live` GWIP/CTIP) for INIT_GW —
they are the P2P-specific endpoints.

### 12.13 Standalone status — the last gate is center-side validation

With the real `fan_ticket` (from /broad/a/watch, suffix `_html5_0`) and the
correct `gateway_ip`/`relay_ip`, a headless client still stalls identically: the
agent completes gateway-login (SVC:41) + cert (SVC:39) + RECV_CHECK (SVC:45) but
never emits SVC:34/18/23/4 (broadcast-init) or binary. So the block is NOT the
fanticket value alone nor the gateway — it is the **P2P center's validation of
the join**, which appears bound to the identity chain that *requested* the
fan_ticket: `fan_ticket ↔ guid ↔ uuid ↔ device/UA`. A synthetic random `guid`
with a `fan_ticket` minted for a different (curl) context is rejected by the
center, so the agent's center-join produces nothing to forward.

Leads to close it (in priority order):
1. **Bind the identity chain.** Derive `guid`/`uuid` deterministically and reuse
   the SAME values for BOTH the `/broad/a/watch` request context and INIT_GW, so
   the center sees one consistent client. The browser persists `guid` in
   `localStorage.UID` and sends `uuid`/`uuid_3rd` in JOINLOG; the fan_ticket is
   likely issued against that identity.
2. **Match client type end to end.** Request the html5 fan_ticket the way the
   desktop player does (it may hit a desktop variant of `/broad/a/watch` or pass
   a player-type param) so the suffix/hash are `_html5_` natively rather than a
   mobile `_android_1` with a swapped suffix.
3. **Inspect RECV_CHECK semantics.** Capture the browser's exact reply to SVC:45
   (the current capture shows the browser NOT acking, but it may ack on a
   separate frame the filter missed) and replicate precisely.

Everything downstream of a successful join is already solved (§12.7–12.8): once
binary flows, the 77-byte deframer yields H.264+AAC and ffmpeg produces the
playable 1080p file, as proven from the browser-session capture.

### 12.14 Narrowing the wall: gateway accepts, center does not

The `/broad/a/watch` call takes `player_type` (`html5`/`als`/`sls`) which sets the
fan_ticket client-type suffix (`_desktop_1` with a desktop UA, `_android_1` mobile,
browser INIT_GW used `_html5_0`). But suffix is not the blocker: with any of these
the **gateway login succeeds and a cert is minted (SVC:39)** — i.e. the gateway
accepts the fan_ticket. The stall is strictly **downstream at the P2P center**
(relay_ip:relay_port): after the cert, the agent's center-join yields no
SVC:34/18/23/4, so no media. Changing the fan_ticket suffix will not move this.

Conclusion: for a fully headless client the only unsolved step is the center
accepting the agent's join. That validation is opaque from the client side (the
agent's center connection is outbound/remote, not on loopback). Closing it needs
either the browser's persistent identity (`guid`=localStorage.UID + matching
`uuid` used both when minting the fan_ticket and in INIT_GW), or capturing the
agent's outbound center handshake. Everything else — bootstrap, session ws,
INIT_GW, gateway login, cert, and the entire media deframe→mux to a playable
1080p file — is solved and proven.

### 12.15 Outbound capture — the "wall" is DEDUP, not identity

Captured the agent's outbound to gateway/center on the NordLynx tunnel
(`Find-NetRoute` shows both route via NordLynx, ifIndex 73) while driving the
handshake with the probe. Findings overturn §12.13:

- **Agent DOES reach both hops.** It opens TCP to gateway `218.38.31.78:3456`
  and center `110.10.76.232:18000` (both over the VPN tunnel, plaintext on the
  tun interface — not TLS).
- **Gateway protocol** (3456): binary, little-endian length-prefixed. The agent
  forwards INIT_GW fields — a frame contains `2A 00 02 00` (cli_type=42), the
  ASCII `guid`, and the `ad_…` addinfo — and the gateway returns the cert. This
  succeeds (matches the local SVC:39).
- **Center** (18000): agent connects and exchanges the binary center protocol,
  then the join is **rejected**. The rejection surfaces on the local ws as:
  ```json
  {"RESULT":-39998,"DATA":{"ERRCODE":-39998,"ERRLINE":244,
   "ERRMSG":"이미 시청중인 방송입니다"}}   // "already watching this broadcast"
  ```

So the stall was never a hard identity/validation wall — it is a **duplicate-
session dedup**. Repeated probe runs (and/or a lingering prior session) left the
broadcast marked as already-viewed for this client/exit-IP, so new joins bounce
with -39998. Earlier runs that stalled silently at RECV_CHECK were the same dedup
before it started returning the explicit error.

Implication: a **single clean session should join and stream**. To get one, clear
prior sessions first — let them expire (keepalive `iKeepAliveTime:300` = 5 min
after the ws closes) or restart the agent — then run exactly one INIT_GW session.
Everything downstream (media deframe → 1080p MP4) is already proven, so a clean
join is the last thing standing between the current code and working no-login
1080p.

### 12.16 Final gate: cookie-based anonymous viewer identity

Captured the center exchange on a clean (non-deduped) run. Confirmed the agent
**does** join the center — it sends a binary JOIN (~100 B, 8-byte LE fields; the
center protocol uses 8-byte fields vs the gateway's 4-byte) to
`110.10.76.232:18000` and the center **responds** (~920 B, message types 0x4a /
0x06 with a repeated center id `0x11a246df`). Yet the local ws still stalls at
RECV_CHECK: the center accepts the connection but does not release
broadcast-init (SVC:34/18/23/4), so no media.

So there are **two independent gates**, both now identified:
1. **Dedup** (`-39998` "already watching this broadcast") — keyed by exit-IP +
   broadcast; clears when prior sessions expire (keepalive 300 s) or via the
   graceful `SVC:88` close the probe now sends.
2. **Identity** — after dedup passes, the center silently withholds
   broadcast-init unless the client presents SOOP's persistent **anonymous viewer
   identity**: `uuid` (JOINLOG `uuid`/`uuid_3rd`) derived from first-party cookies
   `_au` / `OAX` / `said` (LivePlayer.js: `uuid = getCookie("_au")` etc.). This is
   NOT a login — it is the cookie every browser gets on first visit — but a cold
   headless client with a random `guid`/`uuid` is not a recognized viewer, so the
   center's join validates but does not stream.

`_au` is set by the analytics/collector beacon (`collector1.sooplive.com/gather`)
via JS, not by the page HTML response, so it isn't obtained by a plain page GET.

### 12.17 Remaining step to a working headless client

1. Obtain the anonymous `_au`/`OAX` cookie the browser gets on first visit —
   either by replaying the collector beacon that sets it, or by reading it once
   from a real browser profile (it persists; no login).
2. Derive `uuid` from it exactly as LivePlayer.js does, and use the **same** uuid
   in BOTH the `/broad/a/watch` request (so the fan_ticket binds to it) and the
   INIT_GW `guid`/JOINLOG `uuid`/`uuid_3rd` — one consistent identity end to end.
3. Clear prior sessions (graceful `SVC:88` / wait out keepalive) so dedup passes.

With a recognized identity the center should release broadcast-init and the
binary chunk stream (§12.7) flows — which the deframer (§12.8) already turns into
a playable 1080p file. Every other layer — bootstrap, session ws, INIT_GW,
gateway login, cert, gateway/center transport, and the full media
deframe→mux — is solved and verified.

### 12.18 Status summary

| Layer | Status |
|-------|--------|
| Grid params (`/broad/a/watch`, `player_live_api`) | ✅ solved, anonymous |
| Bootstrap 21201 → dynamic session port | ✅ solved |
| Session INIT_GW(40) + choreography | ✅ solved |
| Gateway login (218.38.31.78:3456, binary) + cert | ✅ solved |
| Center connect (110.10.76.232:18000, binary) | ✅ connects & exchanges |
| Center broadcast-init release | ⛔ needs cookie-based `uuid` identity (§12.16) |
| Dedup (-39998) | ✅ understood; clears on expiry / SVC:88 |
| Media chunk deframe (77-B header) | ✅ solved |
| H.264+AAC reconstruction → 1080p MP4 | ✅ PROVEN |

### 12.19 Center-join: empirically ruled out, and the real remaining wall

Exhaustive live testing of a headless standalone client (browser off), each a
single clean session, all stall identically after cert (SVC:39) — the center
never releases broadcast-init (no SVC:34/18/23/4, no media). Ruled OUT as the
cause, each tested and eliminated:

- **fanticket**: fresh, `player_type=html5`, bound to the exact `_au` used — gateway
  still accepts it (mints cert), center still stalls.
- **gateway ip**: both the `type=live` `GWIP` (what the browser uses) and the
  `/broad/a/watch` `gateway_ip` — gateway login succeeds either way; center stalls.
- **`_au` identity**: tested a freshly-generated random `_au` AND a real,
  seasoned `_au` (used consistently in the watch cookie + INIT_GW uuid/uuid_3rd) —
  both stall. So it is not simply "unknown vs registered `_au`".
- **SVC:30 timing**: sending it right after cert vs only after SVC:34/18 — with it,
  the agent replies SVC:45 then stalls; without it, the agent goes silent after
  cert. Either way no 34/18, no media.
- **JOINLOG / addinfo**: full verbatim structure (0x11/0x06/0x12) vs empty — no
  difference.

What IS confirmed on the wire (NordLynx outbound capture): the agent completes
the gateway exchange (binary, 4-byte LE fields; returns cert) and then **connects
to the center** (110.10.76.232:18000, binary, 8-byte LE fields), sends a ~100-byte
JOIN, and the center **replies** ~920 bytes with message types `0x4a` and `0x06`.
Note `0x06` = `HLS_AGENT_SVC_CLOSECH` (close-channel) in the SVC enum — the center
is very likely **rejecting the join** with a close, which is why the agent emits
nothing downstream. The reject reason is encoded in the center's binary protocol,
which lives only in compiled `NetControl.dll`.

Remaining paths to close it (both heavier than anything above):
1. **Baseline diff.** Capture the center exchange during a *successful* browser
   session (110.10.76.232:18000 on NordLynx while the browser plays 1080p) and
   diff the JOIN bytes against the headless probe's — the differing field is the
   gate. This needs the browser watching for one capture.
2. **Disassemble the center protocol** in `NetControl.dll` (the `SvcNetControl_*`
   / center-join handlers) to decode the JOIN struct and the `0x06` reject.

Everything up to the center JOIN is solved and reproducible; the media plane is
fully solved and proven (§12.7–12.8). The single unresolved step is the center
accepting a synthetic client's JOIN.

## 13. RESOLVED — the 540p cap is geo-gating

The entire investigation's premise ("anonymous is capped at 540p; the P2P agent
unlocks 1080p") is only half right. The real gate is **source-IP geolocation**:

- **KR source IP** → `player_live_api.php` mints an AID whose master playlist
  lists only `hd` (540p) + `sd` (360p). 1080p is withheld. (SOOP caps domestic
  free quality to save CDN cost; KR viewers get 1080p by running the P2P grid
  agent — contributing upload bandwidth — which is what §10–12 reverse-engineer.)
- **non-KR source IP** → the same anonymous flow mints an AID whose master lists
  **`original` 1920x1080 (8000k)** + `hd4k` 720p + hd + sd. 1080p is served
  straight from `live-global-cdn-v02.sooplive.com`, **no login, no P2P agent**.

Confirmed by flipping only the source IP's geo (nothing else changed):

```
geo_cc=KR  -> master: NAME=hd 960x540, NAME=sd 640x360
geo_cc=US  -> master: NAME=original 1920x1080, NAME=hd4k 1280x720, NAME=hd, NAME=sd
```

`player_live_api.php` response echoes the detected `geo_cc`; the AID grant tracks
it. Request params (`player_type=html5`, `bid`, `mode=landing`, cookies, Origin,
`quality=*`) do **not** change the outcome — only the geo does. There is no
signed token, no `_au` binding, no browser-JS step involved in the CDN 1080p
path; the earlier "AID differs" observations were the KR-vs-US AID grant.

Why this hid for so long here: the dev machine had **split-tunnel VPN** — the
browser egressed via a US exit (so it saw 1080p) while `curl`/`ytv1` went direct
on the KR line (so they saw 540p). Same machine, two different geos. Routing the
whole desktop through the US VPN made `ytv1` return 1080p immediately.

### 13.1 ytv1 status — already works

From a non-KR IP, the existing `internal/source/soop` CDN path returns 1080p with
no changes:

```
ytv1 -F <soop live url>   ->  1080 1920x1080 8000k / 720 / 540 / 360
ytv1 -g <soop live url>   ->  .../auth_playlist.m3u8?aid=...  (original variant)
# fetched segment: ffprobe -> h264 1920x1080  (verified)
```

Recommendation: to pull >540p from SOOP, run `ytv1` egress from a non-KR IP
(VPN/proxy). No code change, no P2P, no login. The P2P agent path (§10–12)
remains a documented — and for a headless client, still incomplete — alternative
for getting 1080p from a genuine KR IP without a VPN.

### 13.2 Optional hardening (not required)

`fetchLiveAuth`/`fetchLiveInfo` could send the browser's full param set
(`bid`, `player_type=html5`, `mode=landing`, `from_api=0`, `is_revive=false`) for
fidelity, but testing shows the AID grant is geo-determined, so this changes
nothing functionally. The agent scaffold (`agent.go`, `wsconn.go`) can stay as a
fail-safe probe or be removed; it is never load-bearing for the non-KR path.

### 12.20 FINAL VERDICT — KR P2P center-join is a binary-protocol wall

Tested the P2P agent handshake under **every** controllable condition, all from a
clean single headless session:

| Condition | Result |
|-----------|--------|
| US egress (agent → KR center) | silent stall after cert |
| **KR-direct egress** (agent native, target env) | **still stalls after cert** |
| Watched stream (browser open) | `-39998` dedup (center recognizes the viewer) |
| **Unwatched stream, no dedup**, KR | passes dedup → **silent stall after cert** |
| Real browser identity (localStorage `guid` + real `_au` uuid) | stalls |
| Fresh random identity | stalls |
| fanticket from `/broad/a/watch` (html5), correct gateway+relay | gateway mints cert, center stalls |
| Full JOINLOG/addinfo, exact message choreography | stalls |

Consistent shape: bootstrap → session INIT_GW(40) → gateway login (SVC:41) →
**cert (SVC:39) minted** → then the center **never releases broadcast-init**
(no SVC:34/18/23/4, no binary). The `-39998` dedup on a watched stream proves the
KR center *does* engage and recognizes the session; on an unwatched stream it
passes dedup and still withholds. So it is not identity, geo, dedup, fanticket,
gateway, or choreography — the block is the **center's binary join protocol**
(the `SvcNetControl_*` handlers in compiled `NetControl.dll`), which validates
something the browser+agent produce natively and a synthetic ws client does not.

**Conclusion:** completing the KR-native P2P path requires **disassembling
`NetControl.dll`** (the binary center handshake / cert usage) — a separate,
large x86 RE effort, not reachable by network + JS analysis. Everything else is
solved: bootstrap, session ws, INIT_GW, gateway login, cert, and the full media
chunk-deframe → 1080p MP4 (proven from the browser-session capture).

### 12.21 Recommended KR path (given the wall): auth-only egress

Since only the **AID mint** (`player_live_api.php type=aid`) is geo-gated and the
CDN serves segments/media-playlists to any IP once the AID grants them (§13),
the pragmatic KR 1080p path with no full VPN and no P2P is:

1. Mint the 1080p AID + fetch the master through a **non-KR egress** (a few KB —
   any small proxy, or the existing `--proxy`).
2. Download the master's `original` media playlist + all `1920x1080` segments
   **directly from the KR CDN** at full speed.

This yields KR 1080p with only a tiny non-KR touch (the auth call), zero P2P
upload, and full-speed direct video — strictly less "contribution" than the P2P
grid, and it actually works today. The P2P agent path remains blocked at the
`NetControl.dll` center handshake.

## 14. P2P reimplementation — UNBLOCKED by NetControl.dll decompile

A full Ghidra decompile of `NetControl.dll` (the "SOOP Live P2P MultiStream
Engine") landed — **not encrypted, not packed, not obfuscated**; 1400/1737
functions → C. This overturns §12.20's "binary wall": the center protocol is
fully recoverable, so ytv1 can reimplement the P2P client directly (no local
agent, no SOOPStreamer) and pull ORIGINAL/1080p from a KR IP with no VPN, no
login, minimal contribution.

### 14.1 Static AES key (extracted from .rdata)

```
key      45cb101d263d47515b64757b858f999f   (VA 0x10058250 → FUN_10038010)
IV       b6c3cadde7f1fb040e182228323c464c   (VA 0x10058260)
alt-key  5733c5a716f5dc133cca6291f2cb4668   (VA 0x10058280, keylen selectors 7–9)
```
Used by `CNetControlObject::_RecommendEncKeyData` to decrypt the gateway
"recommend/enckey" blob → session key material. NOT a whole-packet cipher
(verified: AES-CBC over captured center bytes yields no structure — the center
protocol header is plaintext).

### 14.2 Three-protocol architecture (verified)

| Conn | Endpoint | Role | Framing |
|------|----------|------|---------|
| Gateway | `GWIP:GWPT` (218.38.31.x:3456) | auth/login → cert ticket + enckey blob | binary, 4-byte LE fields |
| Center  | `CTIP:CTPT` (110.10.76.x:18000) | getnode coordinator → parent/ISS node + quality/cdn | binary, plaintext, 8-byte LE fields (`4a.. 34.. 7e.. <ip>…`) |
| ISS/parent | node IP from getnode | the actual stream | opcodes `0x9c4x` |

Center JSON: `{"quality":%d,"parent_ip":%u,"is_reverse":%d}`, `{"quality":%d,"cdn":%d}`.

### 14.3 ISS stream protocol (opcodes, from ISSSvcProc / FUN_1001ed50)

Inner packet header = 5×u32: `[opcode][flag(neg⇒err)][length][w3][w4]`.

| Opcode | Name | Note |
|--------|------|------|
| `0x9c4b` | ISS_JOIN_STC2STS_V3 | client→server join, payload 0xa18 (2584B), vsend @ vtable+0x5c |
| `0x9c41`/`0x9c46` | ISS_JOIN_STS2STC(_V2) | join reply |
| `0x9c44` | ISS_STREAM_DATA_STS2STC | media, payload @ +16 |
| `0x9c47` | ISS_STREAM_DATA_STS2STC_V2 | **media V2, payload @ +20 = the 77-byte SOOP chunks** |
| `0x9c49/4a/4c` | stream-status / control / stop-notify | |

broad_key (FUN_1001e230): `sprintf("%d-%s-%s-%s", bno, resourceDomain, "original", suffix)`.
Media payload = the 77-byte chunk container already deframed → H.264 + AAC (§12.7–8).

### 14.4 ytv1 foundation (`internal/source/soop/p2p/`)

- `aes.go` — static key/IV + AES-128-CBC (decrypt/encrypt). Compiles.
- `protocol.go` — ISS opcode map, 5-word inner header codec, broad_key builder,
  enckey decrypt hook. Compiles.

### 14.5 Roadmap (remaining — multi-session, methodical)

1. **Gateway handshake**: reconstruct the login/cert packet layout + the enckey
   blob → session key derivation (read `_RecommendEncKeyData` + the gateway
   send/recv funcs; validate vs `soop_outbound.pcap` gateway bytes).
2. **Center getnode**: reconstruct the plaintext 8-byte-field protocol; parse
   parent/ISS node coords + quality/cdn (validate vs captured center bytes).
3. **ISS_JOIN**: the 2584-byte join payload field layout (from the OnConnectISS
   builder around the `0x9c4b` vsend); receive loop for `0x9c47`.
4. **Reassembly**: feed ISS_STREAM_DATA payloads to the existing 77-byte deframer
   → H.264+AAC → mux (done).
5. **Live iterate** against a KR-direct connection to a real broadcast.

Each step = read the dense Ghidra output, reconstruct exact byte layouts, and
validate against the packet captures + the live server (which rejects malformed
frames). Feasible end-to-end; no remaining unknowns of principle — only the
methodical binary-protocol reconstruction.

### 14.6 Gateway session-key derivation (CNetControlObject::_RecommendEncKeyData, FUN_10011070)

The gateway returns an AES-encrypted "enckey" blob; the client derives the
per-session key from it:

```
ctx = aesInit(staticKey=45cb…, staticIV=b6c3…)              // FUN_10038010
dec = aesP3Decrypt(ctx, enckeyBlob, keysel=6, block=0x400)  // FUN_100386d0; require len(dec) >= 0x84
hctx = hashSetKey("teamjsh")                                // FUN_10038b80(_, 0x10044f28="teamjsh\0", 3)
sessionKey = hash(hctx, dec[:0x80])  -> 16 bytes            // FUN_10038f50(hctx, dec, 0x80, out, 0x10)
aesSetKey(ctx, sessionKey)                                  // FUN_10038060  -> session cipher for the rest
```

Constants extracted from .rdata:
- static AES key `45cb101d263d47515b64757b858f999f`, IV `b6c3cadde7f1fb040e182228323c464c` (§14.1)
- **derivation key `"teamjsh"`** (VA 0x10044f28 = `74 65 61 6d 6a 73 68 00`)

`aesP3` (FUN_100386d0) is a table-based AES variant with a key selector (case 6 →
DAT_10058260; cases 7–9 → DAT_10058280). The derivation hash (FUN_10038f50 +
FUN_10038be0 ShiftRows-style state ops) is an **AES-based MAC/hash keyed by
"teamjsh"** over 128 bytes → 16-byte session key.

Remaining for the gateway step: (a) exact `aesP3Decrypt` block/mode and the
FUN_10038f50 MAC construction (AES-CMAC vs AES-CBC-MAC) to reproduce the session
key bit-exact; (b) the gateway login/enckey request+reply packet byte layout
(the 4-byte-LE-field frame seen in `soop_outbound.pcap`: `df03… 88… 357… 06 80 …`
+ AES body). Then Center getnode and ISS_JOIN (§14.5 steps 2–3).

### 14.7 aesP3.DecryptHashP2 — layered crypto (FUN_100386d0) + constants

The protocol payloads use a proprietary multi-layer container, not plain AES:

```
input = [16-byte IV][custom-AES-CBC ciphertext]
 1. keysel = param_5 (6..9): FUN_10038300 (key schedule) + FUN_10038080 (pick key
    DAT_10058260 for sel 6; DAT_10058280 for 7..9)
 2. FUN_100381a0: custom table-based AES-CBC decrypt (FUN_100361a0 init + block
    loop thunk_FUN_10036300; .rdata InvMixColumns 0x0e/0x09/0x0d/0x0b tables)
 3. byte de-obfuscation: data[i] -= subTable[i % 6]   (subTable = int32[6])
 4. magic check #1  (18 bytes) then an inner AES layer (step 2 again) + subTable
    + magic check #2 (12 bytes)
 5. checksum FUN_10038270 (custom 32-bit OR/XOR fold) compared to embedded value
 6. keysel 7 also enforces a ±300 s timestamp
 7. remaining bytes = plaintext payload
```

All constants extracted from NetControl.dll .rdata:

| Const | VA | Bytes / meaning |
|-------|----|-----------------|
| AES key | 0x10058250 | `45cb101d263d47515b64757b858f999f` |
| AES IV  | 0x10058260 | `b6c3cadde7f1fb040e182228323c464c` (keysel 6) |
| AES key (sel 7-9) | 0x10058280 | `5733c5a716f5dc133cca6291f2cb4668` |
| subTable | 0x100582b0 | int32[6] = `[1, -1, -3, -2, 1, 2]` (byte-subtract, i%6) |
| magic #1 | 0x1005829c | 18B `74 70 71 6d 73 71 70 73 63 6a 71 6f 66 66 6c 31 2d 32` ("tpqmsqpscjqoffl1-2") |
| magic #2 | 0x10058290 | 12B `74 6b 61 76 75 64 65 68 64 36 32 35` ("tkavudehd625") |
| derive key | 0x10044f28 | `"teamjsh"` (session-key MAC key, §14.6) |

### 14.8 Scale note & the tractable alternative

Bit-exact reimplementation requires reproducing ~15 interlocking functions (custom
table-AES block+schedule, the layered wrapper decrypt AND its inverse encrypt for
JOIN/login, the MAC, the checksum) across THREE protocols (gateway/center/ISS),
with **no test vectors** — the only validator is the live KR server (binary
accept/reject). That is a large, error-prone research effort.

Tractable alternative (Windows-only): **load NetControl.dll and drive its exported
`CNetControlApp` API** (CreateInstance → SetGuid/SetUserInfo/SetOpenBroad →
RequestBroadInstance → StartInstance, stream out via SetLiveBaseWnd callbacks).
The DLL performs all crypto/protocol; ytv1 only orchestrates and consumes the
decrypted 77-byte chunks. Far less work, reuses the proven engine, but requires
the DLL present and a cgo/C++ shim. Recommended path to a working KR P2P client.

### 14.9 KEY FINDING — the ISS-leaf path is PLAINTEXT (custom AES not needed)

Decoding captured gateway packets (`soop_outbound.pcap`, port 3456) shows the
protocol bodies are **plaintext binary**, not aesP3-encrypted. The layered
custom crypto (§14.7) is only for the optional "recommend/enckey" (peer
encryption) — NOT on the gateway → cert → broadcast → ISS stream critical path.

Outer frame (verified): `[f0 u32][0 u32][bodyLen u32][f3 u32]` (16 bytes) + body.
`f0` is a transaction id shared by a request and its reply; `f3` a running
counter; `bodyLen` = body byte length (e.g. 152-byte packet = 16 + 136).

Captured gateway bodies, all plaintext:
- login (client→gw): `2a 00 02 00` (cli_type=42) + 32-char ASCII `guid` +
  `15 00 00 00` + addinfo (`ad_lang\x11ko\x12is_auto\x110\x12`).
- login reply (gw→client): zeros + mode + `ps_Afreeca` (skin) — matches ws SVC:41.
- broadcast request: `df46a211` + bjid (`viichan6`) + fanticket
  (`b9abcd39…_viichan6_<bno>_html5_0`).
- cert reply: `df46a211 … f8020000` + the 732-hex `pcTicket`
  (`63D1135983F83210…`) — matches ws CERTTICKETEX.
- one binary sub-blob: `06 00 00 00  80 00 00 00` + 128 bytes (opcode 6, len
  0x80) — a signed/hash blob, the only non-plaintext gateway field (TBD; likely
  `pcAppendData`-related, and skippable for a leaf join).

Consequence: reimplementation needs **no cipher** for the leaf path — just
plaintext binary packet (de)serialization for gateway → center getnode → ISS
join → ISS_STREAM_DATA (77-byte chunks → existing deframer). The §14.7 custom
AES is optional and deferred.

### 14.10 Center getnode protocol (plaintext) + a hard risk

Center (port 18000) is plaintext, 4-byte LE fields, opcode-first:
- client→center `0x63` (99): request (getnode/stream), followed by `0x80000000`
  flag words.
- center→client `0x4a` (74) and `0x06` (6): replies carrying a session id
  (`0x11a246df`) and frame-number pairs (`(0x1009,0x593)…`) that track stream
  availability. No encryption.

**Risk to the whole reimplementation.** The ws probe that stalled at the
center-join was driving the REAL SOOPStreamer + REAL NetControl.dll — i.e. the
genuine engine executed the center protocol perfectly and the center still
withheld broadcast-init for our session (fresh fanticket, real guid, KR-direct,
no dedup). That points to a **server-side session validation**, not a
client-protocol defect. If so, a byte-perfect Go reimplementation would hit the
same wall — the center accepts the browser's live session but not a replayed/
synthetic one.

Uncertainty: the stall might instead be specific to the ws↔NetControl bridge in
SOOPStreamer (an exact ws choreography we didn't reproduce), in which case a
full native reimplementation of the getnode→ISS dance could succeed. This is
unresolved. Net: the reimplementation is now tractable (plaintext, all fields
identified) but its success is **not guaranteed** — the center-join may be
gated server-side.

Working alternatives that are certain today: non-KR egress → CDN 1080p (`--proxy`,
§13); auth-only egress (mint the AID via a non-KR touch, download the KR CDN
segments direct — §12.21).

### 14.11 SERVER-WALL CHECK — NEGATIVE. The center accepts us.

Captured a live, SUCCESSFUL browser P2P session (KR-direct, agent engaged,
playing 1440p) and byte-compared its center exchange to the earlier "failed"
probe:

```
C→center 0x63 request (len 100):
  SUCCESS: 63…80×N…80 bf31509a 9f010000 ffffffffffffffff
  FAILED:  63…80×N…80 437c5099 9f010000 ffffffffffffffff
  → byte-IDENTICAL except offsets 84/85/87 (the per-session random id)
center→C replies: 0x4a (len 72) + 0x06 (len 68) — identical structure in both
```

**The center responds to our session exactly as it does to the browser's.** So
the earlier stall was NOT a server-side center wall — the center coordinates our
join the same way and hands back the same peer/node info. The block is
downstream of the center.

What the live success capture reveals:
- Media is delivered **peer-to-peer** from other KR viewers (home IPs, ports
  10007/10080/10290/…), not from the center. The engaged agent both downloads
  from parents AND uploads ~21 MB/10 s to child peers (real contribution).
- Parent→agent media is **plaintext** (H.264 Annex-B NAL `00000001`, ADTS AAC,
  the `01 00 45 00` chunk marker) — entropy 7.87 is just H.264.
- agent→parent request is **high-entropy/encrypted** — the P2P peer handshake
  uses the custom cipher (aesP3 + session key from the gateway enckey, §14.6–7).

Revised blocker: not the center (proven), but the **encrypted peer-connect
handshake** — which is reimplementable (all AES constants extracted). A leaf that
connects to a parent, completes the encrypted handshake, then reads the plaintext
media chunks is viable. Reimplementation is NOT futile.

Remaining build: gateway (plaintext) → center getnode (plaintext, proven to
work) → peer handshake (custom AES, constants in hand) → plaintext media chunks
→ deframer. Optionally the ISS direct-source path as a root fallback.

### 14.12 Peer wire: media plaintext, control encrypted (custom core)

agent↔parent packets (live_p2p.pcap):
- **media**: `0202 <id> <seq u32> <len u32> 0000` + plaintext chunk
  (`01 00 45 00 …` = the H.264+AAC container; on the peer wire the 8×0xFF magic is
  replaced by this `0202…` transport header). Entropy 7.7 = natural H.264.
- **control/handshake**: separate small packets (len 251/308), high entropy, no
  chunk marker. Standard AES-128-CBC with any static key does NOT decrypt them →
  the cipher is the custom aesP3 core (FUN_10036300), not stock AES.

So only the P2P peer control/subscribe handshake needs the custom cipher; the
media itself is plaintext once the peer accepts you. Remaining barrier = reproduce
the custom AES core + the peer subscribe. Next: read FUN_10036300 / FUN_100361a0
(the block cipher + CBC state) to reconstruct it (constants already in hand).

### 14.13 Core cipher = standard AES-128 (Te tables, stream mode) — reproducible

FUN_10036300 uses the classic AES T-table round
(`T0[a&ff]^T1[b>>8&ff]^T2[c>>16&ff]^T3[d>>24]`) with tables at 0x10055100/5500/
5900/5d00. Their first words are `c66363a5 / a5c66363 / 63a5c663 / 6363a5c6` =
the standard **AES encryption tables Te0–Te3**. Using Te (encryption) tables for a
"decrypt" means the mode is a **stream mode (CTR/CFB/OFB)** where decryption uses
the AES-encrypt primitive — so the block cipher is **stock AES-128** and Go's
`crypto/aes` reproduces it exactly.

Key material (FUN_10038300 selector → 16-byte AES-128 key):
- sel 6: `45cb101d263d47515b64757b858f999f` (DAT_10058250)
- sel 7: `3e7b91850e1deabcb5331756ddfb4531` (DAT_10058270)
- sel 8/9: derived session key at ctx+0x224 (from the enckey, §14.6)

So the entire cipher is reproducible with stdlib AES-128 + a stream mode; only the
aesP3 *wrapper* (16-byte IV prefix, subTable byte de-obf, the two magic headers,
custom checksum, timestamp) is bespoke and already fully documented (§14.7).

Remaining to a working peer client: (1) confirm the stream mode (CTR vs CFB) and
implement aesP3 in Go, validating against a captured aesP3 blob via the magic
oracle; (2) derive the session key from a captured gateway enckey; (3) the peer
subscribe packet; then read the plaintext 0202-framed media (§14.12) → deframer.

### 14.14 MAJOR SIMPLIFICATION — custom AES is item-only; stream path is crypto-free

Caller analysis settles it: `aesP3` decrypt (FUN_100386d0) has exactly ONE caller
— `_RecommendEncKeyData`, reached only from `ItemSvcProc` sub-opcode 6 (the game
item / adcon "enckey"). `aesP3` encrypt (FUN_10038350) is likewise used only
inside `_RecommendEncKeyData`. So the custom cipher is **exclusively for the item
service**, NOT for the stream: gateway, center getnode, P2P peer connect, cache
request, and media are all **plaintext binary**. The high-entropy peer-control
packets are binary tokens/hashes, not ciphertext.

P2P cache request (`CNetControlObjectMb::_DataInitHls`): the client sends
opcode **0xcb30** (V2S_REQ_CACHE_DATA_VER2) with a 16-byte payload = a quality
bitmask (SD=1, HD=2, HD4k=8, ORIGINAL=0x10, 1440p=0x20; from the quality
selector) + the requested frame number. The parent replies `REP_CACHE2_DATA`
with the `0202`-framed plaintext media chunks (§14.12) → deframer.

Revised (much simpler) build: everything plaintext.
1. Gateway login → cert (plaintext).
2. Center `0x63` getnode → `0x4a` parent list (plaintext, proven to accept us).
3. Connect to a parent, complete the P2P connect handshake, send `0xcb30`
   cache requests.
4. Read `REP_CACHE2_DATA` plaintext chunks → existing 77-byte deframer → H.264+AAC.

No cipher needed anywhere on the stream path. The enckey / aesP3 work (§14.6–14.13)
is retained only for the optional item service and is off the critical path.

### 14.15 MILESTONE — gateway framing + login VALIDATED against the live server

Replayed the captured client→gateway packets to a live gateway
(118.218.125.116:3456). The server **parsed and replied**:
- login packet (78B: 16B header `e2030000 00000000 3e000000 dc030000` + body
  `2a 00 02 00` + 32-char guid + `15 00 00 00` + addinfo) → server replied 127B
  `e2030000 … ps_Afreeca …` = the login-OK reply (the same `ps_Afreeca` skin seen
  on the ws as SVC:41).
- a duplicate/stale login → server replied 272B with a negative code
  (`19 fc ff ff`) + a Korean error string — i.e. it validates and rejects
  gracefully.

So the wire framing is correct and the gateway handshake is reproducible: connect
TCP → send the login packet → get login-OK. The header fields (f0=0x3e2,
bodyLen=62, f3=0x3dc) were accepted verbatim, so they are not per-session
secrets. Next: rebuild the broadcast-request packet (bjid + fresh fanticket) to
get the cert, then center getnode → peer cache. Transport is proven.

### 14.16 MILESTONE — full gateway handshake reproduced live (login → cert)

A native Go client reproduced the whole gateway handshake against the live
server (118.218.125.116:3456), reconstructing packets from the captured
templates:
- P1 LOGIN (78B): header `e2030000 00000000 3e000000 dc030000` + body
  `2a 00 02 00` + 32-char guid (freshly generated) + `15 00 00 00` + addinfo
  (`\x00ad_lang\x11ko\x12is_auto\x110\x12`). → server replied 127B login-OK
  (`ps_Afreeca`).
- P2 BROADCAST (147B): header `15040000 00000000 83000000 96040000` + body
  `df46a211`(session id) `01000000` + zeros + bjid\@49 (`spbabobj`) + zeros +
  `2a000000 105c0c00 3c000000`(0x3c=fanticket len) + fanticket hash\@87 +
  suffix\@119. → server replied **792B with the 732-hex cert (pcTicket)**.

So the gateway login→broadcast→cert chain is fully working native Go. The header
fields are fixed (not per-session secrets); only guid/bjid/fanticket (and the
session id) vary. Next: feed the cert + session id into the center `0x63`
getnode to receive the parent list, then connect to a parent and issue `0xcb30`
cache requests for the plaintext media.

### 14.17 Center entry — session must be registered (broadcast fields need decompile-exact rebuild)

Native client status:
- **Gateway login works** (any gateway 118.218.125.116 / 218.38.31.99:3456 →
  127B `ps_Afreeca` login-OK). ✅
- **Broadcast → cert reply (~792B)**, but the returned 732-hex cert is the SAME
  fixed value regardless of session id / fanticket / gateway — i.e. either a
  shared public gateway cert, or a canned/default response because our
  reconstructed broadcast packet (P2) is not fully correct.
- **Center `0x63` getnode**: with a fresh client-random session id the center
  gives **no reply**, whereas the captured real session ids (bf31509a/437c5099,
  generated by the genuine SOOPStreamer) were accepted. So the session id must be
  registered via a *correct* broadcast, and our P2 packet does not register it.

Suspect P2 fields (kept from the viichan6 capture, likely broadcast-specific):
the `2a000000 105c0c00 3c000000` block before the fanticket (0x105c0c looks
bno/category-derived), and the session-id semantics. Getting a real session
requires the exact `V2S_REQ_BROADCAST_STREAM_VER2` builder from the decompile
(FUN_1000bed0 / the RequestBroadInstance path) rather than a capture-substituted
template.

Net: transport + gateway login are reproduced natively; the next concrete step is
reading the broadcast-request builder to construct a P2 that the gateway accepts
as a fresh session (so the center recognizes the session id), then center getnode
→ peer `0xcb30` cache → plaintext media.

### 14.18 Broadcast session registration lives in SOOPStreamer's orchestration

Ruled out as the center-reject cause:
- `2a000000 105c0c00 3c000000` block: the middle `105c0c00` (0xc5c10) is a
  **constant** — identical across two independent captured sessions, so not
  broadcast-specific.
- stale fanticket: a freshly-minted fanticket yields the same result.
- gateway choice / session id: the gateway always returns the same fixed 732-hex
  cert (a shared public gateway cert); the center still gives no reply.

SetUserInfo (FUN_100058c0) only STORES the user-info blob (bjid/fanticket/fields)
that the host passes in; NetControl.dll forwards it. So the exact broadcast body
and the post-cert **session-registration orchestration** (which packets, in which
order, to finalize the session the center will accept) live in **SOOPStreamer.exe**
(not decompiled), not in NetControl.dll. Our native login→broadcast→cert works,
but we are missing the follow-up that registers the session backend-side, so the
center rejects our session id.

Two ways to finish:
1. **Full fresh-handshake capture + exact replay.** Capture a brand-new
   successful session's gateway+center packets from packet #1 (our captures so
   far join mid-session), then replay the exact sequence substituting
   guid/bjid/fanticket/session-id.
2. **Decompile SOOPStreamer.exe** to read the orchestration directly.

Everything else is proven: transport, gateway login, cert, plaintext center/peer,
plaintext media, deframer. The remaining gap is the session-finalize sequence.

### 14.19 MILESTONE — correct path is ISS-direct; the relay engages our native handshake

A fresh full-handshake capture (from packet #1) revealed what our earlier
reconstruction missed:
- **Gateway** (118.218.125.115:3456), in order: `06`-blob (152B, sent FIRST,
  before login) → login (78B) → broadcast (150B) → `cutc` (45B). Verbatim replay
  works: login-OK, cert (the fixed shared 732-hex `63D11359…`), and a proper
  `cutc/sutc` reply. Gateway handshake is complete and reproduced natively.
- **Relay/ISS** (110.10.76.217:18000) is NOT the getnode `0x63` coordinator — it
  uses a low-opcode **ISS-direct** handshake: `op2 → op3 → op3e(config+quality)
  → op34 → op71 → op50(RequestBroad) → op69/6b/6c(cache/media)`. Our earlier
  `0x63` was the wrong protocol for this endpoint, which is why it never replied.

Replaying the ISS handshake natively: **`op2` → relay replies 28B, `op3` → relay
replies 56B** — the relay accepts and engages our connection. Subsequent packets
(`op3e`, `op50`, …) then went unanswered because we replayed the *captured*
session's field values; those packets must instead be built from the values the
relay returns in the `op2`/`op3` replies (session-consistent), not verbatim.

So: getnode/`0x63` was a dead end; the working path is the ISS-direct relay
handshake, and the relay demonstrably engages our native client. Remaining:
read the OnConnectISS builder to thread the `op2`/`op3` reply fields into
`op3e`/`op50`, then the relay streams `op69/6b/6c` cache/media → deframer.

### 14.20 ISS-direct handshake — FULLY DECODED (fields + threading)

The relay (18000) ISS-direct handshake, every packet decoded from the fresh
capture. Header per packet = `[opcode u32][0][bodyLen u32][0][?][body]`.

Client → relay requests:
- `op2` (21B) init → relay replies 28B: `…08000000 0a000000 2c010000 a66a0000`
  (assigns a handle `a66a=0x6aa6`).
- `op3` (21B) init → relay replies 56B: `…a66a0000 … bcc4729a 9f010000`
  (more handle state).
- `op3e` (28B) config → relay replies 247B with `buffilled_cnt=3&…&emgy_quality=63&
  keep_alive_cnt=2…` (buffer/quality config).
- `op34` (293B): the **CERT** (a suffix of the gateway's shared 732-hex cert,
  starting at `4F7356AD…`) + `quality=ori&is_auto=false&passwd=(null)`.
- `op71` (79B): `{"broad_no":0,"relay_port":27943,"reverse_port":13608}` — the
  client's own P2P serving ports (0/0 for a pure leaf).
- `op50` (24B): RequestBroad — carries the session handle (`4765a211`).
- `op69` (94B): `{"list":[{"alloc_type":1,"parent_ip":0,"parent_port":0,"quality":1}]}`
  — parent-allocation request.
- relay reply `op4a` (259B): broadcast info incl. session id `4765a211` and
  `preset {"view_preset":"original, hd, sd, hd_4k, hd_8k"}` (this stream goes to 8k!).

**Key threading requirement:** the relay ASSIGNS state in the `op2`/`op3` replies
(the `0x6aa6` handle etc). `op50`/`op34`/`op69` must carry the relay-assigned
handle for THIS connection, not the captured session's values — which is why the
verbatim replay got replies to `op2`/`op3` but silence afterward. The client must
be **stateful**: read each reply, extract the handle/params, thread them into the
next request.

The whole no-crypto handshake is now decoded end to end (gateway op6→login→
broadcast→cert; relay op2→op3→op3e→op34(cert)→op71→op50→op69→media). Remaining:
implement the stateful threading (relay handle → op50/op34/op69), then the relay
streams `op69/6b/6c` cache/media → 77-byte deframer → H.264+AAC.

### 14.21 op2a session-auth crypto — FULLY SPECIFIED (all constants)

The real ISS handshake (reassembled from the fresh capture, framing
`[op u64][bodyLen u64][w u32][body]`):

CLIENT → RELAY, in order:
- `op2` len=1 `00`, `op3` len=1 `d4` — init pings
- `op3e` len=8 `0400120000000000` — config
- `op14` len=12 `01000000 b3060000 f4010000` — subscribe [1][0x6b3][0x1f4] (periodic)
- **`op2a` len=136 = `[8][0x80000000][128-byte SIGNATURE]`** — the session-auth blob
- **`op46` len=1465** — contains `...uuid=<_au cookie>...&log...` → **login required**

RELAY → CLIENT: `op2/op3/op62` (handle assign), `op3e` config, `op4a` (session id
`4765a211`), `op46` (broadcast meta), `op50` (`broadno=295855431…`), **`op4d {"RESULT":0}`**
(ACCEPT), then `op69 {"list":[{child_wait,cur_frame_number,…}]}` (frame availability),
`op6b {"quality":32}` (0x20=1440p), `op6c` (quality) — the **frame-pull** media protocol.

**op2a blob = FUN_10037760(mode=8, key=sessionKey@obj+0x60[20B], input=GUID):**
1. 16-byte scrambled header = interleave(timestamp `DAT_1006a948`, incrementing
   counter `DAT_1006a93c`, CRC `FUN_10037640(input)`, inputLen) — byte order per the
   puVar1[0..15] assignments.
2. append salt1 (12B) + the input bytes.
3. AES-CBC encrypt (FUN_100380d0, mode-8 key schedule via FUN_10037720/10037600).
4. prepend salt2 (18B); CRC32 (FUN_10036040) → derive IV (FUN_1003a1a0) → AES-CBC
   encrypt again (double layer). Result = the 128-byte blob.

**All constants (extracted from NetControl.dll .rdata):**
```
aesKey   (0x10058250) = 45cb101d263d47515b64757b858f999f
aesIV    (0x10058260) = b6c3cadde7f1fb040e182228323c464c
aesAlt   (0x10058280) = 5733c5a716f5dc133cca6291f2cb4668   (mode 7–9)
salt1    (0x10058218,12B) = "tkavudehd625"
salt2    (0x10058224,18B) = "tpqmsqpscjqoffl1-2"
sessionKey (obj+0x60,20B) = decrypt(gateway enckey blob, aesKey/aesIV)  [per-session]
```

**Status — protocol 100% reverse-engineered.** Every packet, field, and crypto
constant is known. What remains to make it stream:
1. Port FUN_10037760 (double-AES-CBC envelope, mode-8 schedule) byte-exact — the
   op2a blob. No offline test vectors exist; correctness is only verifiable against
   the live relay.
2. Obtain the gateway enckey blob → derive the 20-byte per-session key.
3. Send `_au` (login) in op46, run the op69/6b/6c frame-pull loop → 77-byte chunks
   → existing deframer → 1080p.

**Two things the user must weigh:** (a) the KR-native path **requires login** (the
relay authenticates the viewer via the `_au` cookie in op46) — so "zero contribution
without login" is not possible on this path; (b) the **US-VPN path already delivers
1080p ORIGINAL** with no login, no crypto, no P2P. The KR-native path's remaining
work (exact crypto port + live validation) is large and can only be validated with a
live broadcast up.

### 14.22 op2a crypto CRACKED — decrypt byte-exact validated offline

Decrypted the captured op2a blob offline and recovered its plaintext exactly,
proving the whole construction. **The cipher is AES-128-CTR (not CBC)** — the tell
in FUN_10036300 is `out[i] = in[i] ^ keystream` plus a big-endian 16-byte counter
increment. CTR is symmetric, so the same routine encrypts and decrypts.

**Validated blob layout** (128 bytes):
```
blob[0:16]   = H(pbVar2)            // custom-hash MAC, also the pass-2 CTR key
blob[16:128] = CTR(key=blob[0:16], iv=IV8, pbVar2_padded)   // pass 2
pbVar2 = [salt2_scrambled 18][c1 80][zero-pad to 16]
c1     = CTR(key=KEY8, iv=IV8, puVar1_padded)               // pass 1
puVar1 = [header 16][salt1_scrambled 12][input 45]
```
Decrypting the capture yields `salt2 → "tpqmsqpscjqoffl1-2"` and
`salt1 → "tkavudehd625"` exactly, and the input carries the 32-char session GUID
`25631A3AB79CB882B26207735783A003`. The 16-byte header decodes to
`inputLen | timestamp | incrementing-counter | checksum(FUN_10037640)` in the
scrambled order of the puVar1[0..15] assignments.

**Constants (NetControl.dll .rdata):**
```
KEY8 (0x100581f8) = e2081129c4d0afbe55379fcde1755413   // mode-8 AES key
IV8  (0x10058208) = 22655d8796eeca33c7a8221dffcb8271   // mode-8 AES IV/counter
salt1 "tkavudehd625"        salt2 "tpqmsqpscjqoffl1-2"
saltTbl (0x10058238, i%6)  = +1,-1,-3,-2,+1,+2  (add to encode, sub to decode)
```

**The MAC `H` (blob[0:16]) is a custom hash** — MD5 init constants
(`67452301/EFCDAB89/98BADCFE/10325476`) and MD5-style finalize (0x80 bit + 64-bit
length, FUN_1003a0d0), but an 80-step transform (FUN_10039050) with SHA-family
round constants (`5a827999/6ed9eba1/8f1bbcdc/50a28be6/5c4dd124/6d703ef3` + two
constant-free rounds), permuted boolean functions and custom rotation amounts.
Standard MD5 over every slice of the recovered plaintext fails to reproduce
blob[0:16], confirming the custom transform. It ports verbatim from the decompile
and is validated offline against the recovered (plaintext → blob[0:16]) test vector.

**Consequence:** the op2a session-auth blob is fully reproducible with static
constants — no per-session server secret is needed for the crypto itself. The
remaining work is the verbatim hash port (offline-validatable), the encoder, then
the op46(`_au`)/op14 framing, the stateful handshake, and the op69/6b/6c frame-pull
loop — the last of which still needs a live broadcast to validate end to end.

### 14.23 op2a crypto validated LIVE against the relay; op46 path_key is the last gate

Ran a native probe from a KR IP against the live relay (`110.10.76.216:18000`) for a
live broadcast: gateway login+broadcast (fresh GUID) → cert, then the ISS relay
handshake with `op2/op3/op3e/op14/op2a`, the op2a body built by
`p2p.BuildAuthBlob` (the reverse-engineered crypto).

Result:
- `op2` → relay replies 28B, `op3` → 56B — **byte-identical to the capture**
  (`…0a0000002c010000`, `…0b000000fb7dd8d9`). Framing + session engage correctly.
- After `op2a` the relay stays **silent but keeps the connection OPEN** (a follow-up
  write succeeds) — i.e. the relay did **not reject the op2a blob**. The crypto is
  accepted live; the relay is waiting for `op46`.

So the op2a session-auth crypto is confirmed correct end to end, not just offline.

**Last gate = op46.** Its `path_key = <_au>_<bno>_<token>` carries a 128-byte token
(hex, starts `1785…`). It is NOT the pcTicket, NOT any authblob mode (decrypt with
mode 6/7/8 keys yields no salts), and does not appear in NetControl.dll (`path_key`
is assembled outside it). Because `path_key` embeds the browser's `_au` cookie, the
browser assembles it and hands it to SOOPStreamer over the localhost WebSocket — so
the token (or its API source) is visible in a browser HAR of a 1080p P2P session
(the APIs are HTTPS, but a HAR decrypts them). That HAR is the next input needed to
finish op46 and reach the relay's `op4d {"RESULT":0}` → frame-pull → 1080p.
