package soop

// SOOP P2P grid-delivery agent: local endpoints, an availability probe, and the
// small field/parse helpers shared by the agent-streaming path (agentstream.go,
// agentwire.go).
//
// The native SOOP desktop app ("SOOPStreamer.exe") joins SOOP's viewer P2P mesh
// and re-serves the ORIGINAL/1080p live stream — gated away from anonymous CDN
// tokens — over a loopback WebSocket. The streaming path drives that socket
// (docs/SOOP_P2P_GRID_ANALYSIS.md §14.25). Every failure path is non-fatal, so a
// missing or idle agent simply falls back to the CDN route.

import (
	"encoding/json"
	"net"
	"os"
	"strconv"
	"time"
)

// Agent local endpoints and protocol tunables. Vars (not consts) so tests can
// override them.
var (
	agentHost = "127.0.0.1"
	// agentServicePort is the local control WebSocket (nPackagePort). The ws
	// upgrade at /Websocket is accepted here; the per-session service port is
	// returned in-band (HTMLPLAYER_PORT).
	agentServicePort = 21201
	agentWSPath      = "/Websocket"

	agentDialTimeout = 2 * time.Second
)

// Service (SVC) codes exchanged on the agent socket (from the web player's
// HLS_AGENT_* enum) that the streaming choreography sends.
const (
	svcInitBroad    = 4
	svcCertTicketEx = 39
	svcInitGW       = 40
)

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
