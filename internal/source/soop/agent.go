package soop

// SOOP P2P grid-delivery agent driver.
//
// The native SOOP desktop app ("SOOPPackage.exe") joins SOOP's viewer P2P mesh
// and re-serves the stream — including the ORIGINAL/1080p variant that is gated
// away from anonymous CDN tokens — as a plain local HLS playlist at
// http://127.0.0.1:<hlsPort>/<bno>/playlist.m3u8 (CORS *). Driving the agent is
// the only no-login path to >540p.
//
// This talks to the agent's local control socket (ws://127.0.0.1:21201/Websocket)
// with JSON frames {SVC, RESULT, DATA}, reproducing the browser player's
// handshake: INIT_GW -> (CERTTICKETEX ticket) -> INIT_BROAD -> (HLS_PORT).
//
// NOT VIABLE against the current SOOP client; kept as a fail-safe probe only.
// A live loopback capture (docs/SOOP_P2P_GRID_ANALYSIS.md §10) showed the agent
// delivers live media as proprietary *binary WebSocket frames* on a dynamic
// session socket (ws://127.0.0.1:<HTMLPLAYER_PORT>/Websocket/<bjid>), which the
// browser SDK demuxes into MSE — there is no local HLS server to GET in this
// mode (the advertised HLS_PORT never accepts a connection). So this local-HLS
// path resolves nothing and always falls through to the CDN/540p route.
// Consuming the P2P stream would require reimplementing the binary media demux,
// out of scope here. Every failure path is non-fatal, so the dead handshake
// never breaks extraction — it just fast-fails to fallback.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// Agent local endpoints and protocol tunables. Vars (not consts) so tests and
// future capture-based tuning can override them.
var (
	agentHost = "127.0.0.1"
	// agentServicePort is the local control WebSocket (nPackagePort). Confirmed
	// by probe: this port accepts the ws upgrade at /Websocket; the per-session
	// service/HLS ports are returned in-band (HTMLPLAYER_PORT / HLS_PORT).
	agentServicePort    = 21201
	agentWSPath         = "/Websocket"
	agentDefaultHLSPort = 2935

	agentDialTimeout      = 2 * time.Second
	agentHandshakeTimeout = 8 * time.Second
)

// Service (SVC) codes, from the web player's HLS_AGENT_* enum.
const (
	svcKeepAlive    = 1
	svcInitBroad    = 4
	svcCertTicketEx = 39
	svcInitGW       = 40
	svcCloseBroad   = 88
)

// cliTypeGateway identifies the desktop HTML5 agent context to the gateway.
// Best-effort pending live validation.
const cliTypeGateway = 14 // GW_CLIENT_T_HTML5_WINDOW* / CCP_CTYPE_GPC

// agentFrame is one control message on the agent socket.
type agentFrame struct {
	SVC    json.RawMessage `json:"SVC"`
	RESULT int             `json:"RESULT"`
	DATA   map[string]any  `json:"DATA"`
}

func agentEnabled() bool {
	// Opt-out: when the local SOOP agent is running (agentProbe), the agent path
	// pulls the ORIGINAL/1080p P2P stream and remuxes it live to MPEG-TS. Set
	// YTV1_SOOP_NO_AGENT=1 to force the CDN path (≤540p anonymously; higher with
	// login cookies) instead.
	switch os.Getenv("YTV1_SOOP_NO_AGENT") {
	case "1", "true", "TRUE", "yes":
		return false
	}
	return true
}

// agentProbe reports whether the local agent looks available. Indirection so
// tests can force the CDN path without a real SOOP install listening.
var agentProbe = agentReachable

// agentReachable reports whether the SOOP package process is listening locally.
// Cheap TCP probe so extraction skips the handshake entirely when the app is
// not installed/running.
func agentReachable() bool {
	addr := net.JoinHostPort(agentHost, strconv.Itoa(agentServicePort))
	conn, err := net.DialTimeout("tcp", addr, agentDialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// localPlaylistURL is the agent's local HLS master playlist for a broadcast.
func localPlaylistURL(bno string, hlsPort int) string {
	return fmt.Sprintf("http://%s:%d/%s/playlist.m3u8", agentHost, hlsPort, bno)
}

// newGUID returns a random client GUID for the gateway/center handshake. The
// agent only needs a stable-per-session unique id; persistence isn't required.
func newGUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "ytv1-soop-agent"
	}
	return hex.EncodeToString(b[:])
}

// resolveViaAgent performs the full agent handshake for a broadcast and returns
// the local HLS playlist URL serving the highest quality (incl. ORIGINAL).
func (s *Source) resolveViaAgent(ctx context.Context, info *liveInfo) (string, error) {
	if info.CenterIP == "" || info.GateWayIP == "" {
		return "", fmt.Errorf("soop: agent path missing grid coordinates")
	}
	port, err := s.agentHandshake(ctx, info)
	if err != nil {
		return "", err
	}
	return localPlaylistURL(info.BNO, port), nil
}

// agentHandshake runs INIT_GW -> CERTTICKETEX -> INIT_BROAD -> HLS_PORT and
// returns the agent's local HLS port. On success the control socket is kept
// open with a background keepalive loop: the agent tears down the local HLS
// server when the control session drops, so it must live for the download's
// duration. On any failure the socket is closed.
func (s *Source) agentHandshake(ctx context.Context, info *liveInfo) (int, error) {
	host := net.JoinHostPort(agentHost, strconv.Itoa(agentServicePort))
	ws, err := wsDial(host, agentWSPath, agentDialTimeout)
	if err != nil {
		return 0, fmt.Errorf("soop: agent connect: %w", err)
	}
	success := false
	defer func() {
		if !success {
			ws.close()
		}
	}()

	deadline := time.Now().Add(agentHandshakeTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = ws.conn.SetDeadline(deadline)

	guid := newGUID()

	// Step 1: gateway init (HLS_AGENT_GW_SVC_INIT; lowercase-key dialect).
	gwData := map[string]any{
		"gate_ip":     info.GateWayIP,
		"gate_port":   asInt(info.GateWayPort),
		"center_ip":   info.CenterIP,
		"center_port": asInt(info.CenterPort),
		"broadno":     asInt(info.BNO),
		"cookie":      info.Ticket,
		"guid":        guid,
		"cli_type":    cliTypeGateway,
		"passwd":      "",
		"category":    info.Category,
		"BJID":        info.BjID,
		"fanticket":   info.FanTicket,
		"addinfo":     "",
		"update_info": 0,
		"JOINLOG":     "",
	}
	if err := s.agentSend(ws, svcInitGW, gwData); err != nil {
		return 0, err
	}

	// Step 2: await the cert ticket the gateway mints for the center join.
	gwTicket, appendData, err := s.awaitCertTicket(ws)
	if err != nil {
		return 0, err
	}

	// Step 3: init broadcast on the center (HLS_AGENT_SVC_INIT_BROAD;
	// uppercase-key dialect) using the minted ticket.
	broadData := map[string]any{
		"CIP":       info.CenterIP,
		"CPORT":     asInt(info.CenterPort),
		"BNO":       asInt(info.BNO),
		"PARENTBNO": 0,
		"SCRAPBJ":   false,
		"PASSWORD":  "",
		"GUID":      guid,
		"TICKETLEN": len(gwTicket),
		"TICKET":    gwTicket,
		"QUALITY":   "ori", // ORIGINAL
		"APPDATA":   appendData,
		"BJID":      info.BjID,
		"SPROTOCOL": "hls",
		"JOINLOG":   "",
	}
	if err := s.agentSend(ws, svcInitBroad, broadData); err != nil {
		return 0, err
	}

	// Step 4: await the local HLS port.
	port, err := s.awaitHLSPort(ws)
	if err != nil {
		return 0, err
	}

	// Keep the session alive for the download. Clear the handshake deadline and
	// hand the socket to a background keepalive loop.
	_ = ws.conn.SetDeadline(time.Time{})
	success = true
	go s.agentKeepAlive(ws)
	return port, nil
}

// agentKeepAlive services the control socket for the lifetime of the stream:
// it answers the agent's keepalive frames and returns (closing the socket) once
// the connection errors or the agent ends the broadcast. This goroutine
// intentionally outlives Extract — it is bound to the download, not extraction,
// and is reclaimed when the process exits or the agent drops the stream.
func (s *Source) agentKeepAlive(ws *wsConn) {
	defer ws.close()
	for {
		frame, err := s.agentRead(ws)
		if err != nil {
			return
		}
		switch svcOf(frame) {
		case svcKeepAlive:
			if err := s.agentSend(ws, svcKeepAlive, map[string]any{}); err != nil {
				return
			}
		case svcCloseBroad:
			return
		}
	}
}

func (s *Source) agentSend(ws *wsConn, svc int, data map[string]any) error {
	payload, err := json.Marshal(map[string]any{
		"SVC":    svc,
		"RESULT": 0,
		"DATA":   data,
	})
	if err != nil {
		return err
	}
	if err := ws.writeText(payload); err != nil {
		return fmt.Errorf("soop: agent send SVC=%d: %w", svc, err)
	}
	return nil
}

// awaitCertTicket reads frames until one carries the gateway cert ticket
// (pcTicket/pcAppendData), answering keepalives along the way.
func (s *Source) awaitCertTicket(ws *wsConn) (ticket, appendData string, err error) {
	frame, err := s.readUntil(ws, 32, func(f *agentFrame) bool {
		_, ok := stringField(f.DATA, "pcTicket")
		return ok || svcOf(f) == svcCertTicketEx
	})
	if err != nil {
		return "", "", fmt.Errorf("soop: agent cert ticket not received: %w", err)
	}
	t, ok := stringField(frame.DATA, "pcTicket")
	if !ok {
		return "", "", fmt.Errorf("soop: agent cert ticket frame missing pcTicket")
	}
	ad, _ := stringField(frame.DATA, "pcAppendData")
	return t, ad, nil
}

// awaitHLSPort reads frames until one carries HLS_PORT.
func (s *Source) awaitHLSPort(ws *wsConn) (int, error) {
	frame, err := s.readUntil(ws, 64, func(f *agentFrame) bool {
		_, ok := intField(f.DATA, "HLS_PORT")
		return ok
	})
	if err != nil {
		return 0, fmt.Errorf("soop: agent HLS port not received: %w", err)
	}
	if p, _ := intField(frame.DATA, "HLS_PORT"); p > 0 {
		return p, nil
	}
	return agentDefaultHLSPort, nil
}

// readUntil reads up to max frames, auto-answering keepalives, and returns the
// first frame satisfying match.
func (s *Source) readUntil(ws *wsConn, max int, match func(*agentFrame) bool) (*agentFrame, error) {
	for range max {
		frame, err := s.agentRead(ws)
		if err != nil {
			return nil, err
		}
		if svcOf(frame) == svcKeepAlive {
			if err := s.agentSend(ws, svcKeepAlive, map[string]any{}); err != nil {
				return nil, err
			}
			continue
		}
		if match(frame) {
			return frame, nil
		}
	}
	return nil, fmt.Errorf("soop: no matching frame within %d reads", max)
}

// svcOf extracts the numeric SVC code from a frame, tolerating string-encoded
// codes the agent may use on some channels.
func svcOf(f *agentFrame) int {
	if f == nil || len(f.SVC) == 0 {
		return -1
	}
	var n int
	if err := json.Unmarshal(f.SVC, &n); err == nil {
		return n
	}
	return -1
}

func (s *Source) agentRead(ws *wsConn) (*agentFrame, error) {
	raw, err := ws.readText()
	if err != nil {
		return nil, err
	}
	var frame agentFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		// Unparseable frame: skip by returning an empty frame so the caller's
		// loop keeps reading rather than aborting the handshake.
		return &agentFrame{}, nil
	}
	return &frame, nil
}

// ---- small helpers ----

func asInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func orString(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func stringField(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	switch t := v.(type) {
	case string:
		if t == "" {
			return "", false
		}
		return t, true
	case json.Number:
		return t.String(), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	}
	return "", false
}

func intField(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case json.Number:
		n, err := t.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(t)
		return n, err == nil
	}
	return 0, false
}
