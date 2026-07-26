package soop

// Agent-streaming path: drive the local SOOP P2P agent (SOOPStreamer) over its
// loopback WebSocket to pull the ORIGINAL/1080p live stream, deframe the binary
// media, and remux it to a fragmented MP4. This is the no-VPN, KR-native ≥1080p route;
// the agent joins the P2P grid and the crypto/relay work happens inside it.
//
// The full handshake + message choreography was reverse-engineered and verified
// live (docs/SOOP_P2P_GRID_ANALYSIS.md §14.25):
//
//	ws 127.0.0.1:21201  -> CAPTION            -> HTMLPORT{HTMLPLAYER_PORT}
//	ws 127.0.0.1:<port>/Websocket/<bjid>:
//	  >> SVC:40 INIT_GW      >> SVC:51 initializing
//	  << SVC:41 login        << SVC:39 CERTTICKETEX{pcTicket,pcAppendDat,iPort,uiIpAddr}
//	  >> SVC:4  center-init  (echo TICKET+APPDATA + SIP/SPORT/SPROTOCOL/BJID/…)
//	  >> SVC:30 UID          ; heartbeat SVC:52 @200ms throughout
//	  << SVC:4  (ADDINFO)     >> SVC:5 START   >> SVC:500 cutc
//	  << SVC:50 status:1      << binary media (77-byte chunks)

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

// SVC codes used by the agent-streaming choreography.
const (
	svcStart        = 5
	svcUID          = 30
	svcUTC          = 500
	svcHeartbeat    = 52
	svcInitializing = 51
	svcAddInfoRecv  = 4 // agent->client with ADDINFO (preset) triggers START
)

// agentStreamParams holds everything the choreography needs.
type agentStreamParams struct {
	BNO         string
	BjID        string
	GUID        string
	FanTicket   string
	AU          string // _au cookie value → JOINLOG uuid
	GateWayIP   string
	GateWayPort string
	CenterIP    string
	CenterPort  string
	Category    string
}

// streamAgentMedia runs the agent handshake and writes a remuxed fragmented MP4 to out
// until the stream ends or ctx is cancelled. It blocks for the stream's lifetime.
func (s *Source) streamAgentMedia(ctx context.Context, p agentStreamParams, out io.Writer) error {
	// 1. Bootstrap the control port to obtain a dynamic session port.
	host := agentHost + ":" + strconv.Itoa(agentServicePort)
	boot, err := wsDial(host, agentWSPath, agentDialTimeout)
	if err != nil {
		return fmt.Errorf("soop: agent bootstrap connect: %w", err)
	}
	if err := boot.writeText([]byte(`{"SVC":"CAPTION","RESULT":1,"DATA":{"nCaption":5}}`)); err != nil {
		boot.close()
		return err
	}
	raw, err := boot.readText()
	boot.close()
	if err != nil {
		return fmt.Errorf("soop: agent HTMLPORT: %w", err)
	}
	var hp struct {
		DATA struct {
			HTMLPLAYERPORT int `json:"HTMLPLAYER_PORT"`
		} `json:"DATA"`
	}
	_ = json.Unmarshal(raw, &hp)
	if hp.DATA.HTMLPLAYERPORT <= 0 {
		return fmt.Errorf("soop: agent returned no session port")
	}

	// 2. Session socket.
	sessHost := agentHost + ":" + strconv.Itoa(hp.DATA.HTMLPLAYERPORT)
	ws, err := wsDial(sessHost, agentWSPath+"/"+p.BjID, agentDialTimeout)
	if err != nil {
		return fmt.Errorf("soop: agent session connect: %w", err)
	}
	defer ws.close()

	if err := s.sendSVC(ws, svcInitGW, p.initGWData()); err != nil {
		return err
	}
	if err := s.sendSVC(ws, svcInitializing, map[string]any{"BUFFERING_CAUSE": "initializing"}); err != nil {
		return err
	}

	// Heartbeat loop keeps the session alive; stops when the stream ends.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				_ = s.sendSVC(ws, svcHeartbeat, map[string]any{"BUFFER_LEFT": 0})
			}
		}
	}()

	mux, err := startESMux(out)
	if err != nil {
		return err
	}
	defer mux.close()

	// Watcher: on ctx cancellation, nudge the read deadline so a blocked read
	// unblocks and the loop returns promptly (the mux is finalized by the defer).
	go func() {
		<-ctx.Done()
		_ = ws.conn.SetReadDeadline(time.Now())
	}()

	var d deframer
	started := false

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = ws.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		op, payload, err := ws.readAny()
		if err != nil {
			return err
		}
		if op == wsOpBinary {
			var werr error
			d.push(payload, func(kind chunkKind, es []byte) {
				if werr != nil {
					return
				}
				switch kind {
				case chunkVideo:
					werr = mux.writeVideo(es)
				case chunkAudio:
					werr = mux.writeAudio(es)
				}
			})
			if werr != nil {
				return werr
			}
			continue
		}
		// text control frame
		var m struct {
			SVC  int            `json:"SVC"`
			DATA map[string]any `json:"DATA"`
		}
		if json.Unmarshal(payload, &m) != nil {
			continue
		}
		switch m.SVC {
		case svcCertTicketEx: // 39
			if err := s.sendSVC(ws, svcInitBroad, p.centerInitData(m.DATA)); err != nil {
				return err
			}
			if err := s.sendSVC(ws, svcUID, map[string]any{"UID": ""}); err != nil {
				return err
			}
		case svcAddInfoRecv: // 4
			if _, ok := m.DATA["ADDINFO"]; ok && !started {
				started = true
				if err := s.sendSVC(ws, svcStart, map[string]any{}); err != nil {
					return err
				}
				_ = s.sendSVC(ws, svcUTC, map[string]any{"cutc": float64(time.Now().Unix())})
			}
		}
	}
}

func (s *Source) sendSVC(ws *wsConn, svc int, data map[string]any) error {
	b, err := json.Marshal(map[string]any{"SVC": svc, "RESULT": 0, "DATA": data})
	if err != nil {
		return err
	}
	if err := ws.writeText(b); err != nil {
		return fmt.Errorf("soop: agent send SVC=%d: %w", svc, err)
	}
	return nil
}

func (p agentStreamParams) initGWData() map[string]any {
	return map[string]any{
		"gate_ip": p.GateWayIP, "gate_port": asInt(p.GateWayPort),
		"center_ip": p.CenterIP, "center_port": asInt(p.CenterPort),
		"broadno": asInt(p.BNO), "cookie": "", "guid": p.GUID, "cli_type": 42,
		"passwd": "", "category": p.Category, "JOINLOG": p.joinlog("-1"),
		"BJID": p.BjID, "fanticket": p.FanTicket,
		"addinfo": "ad_lang\x11ko\x12is_auto\x110\x12", "update_info": 0,
	}
}

// centerInitData builds the SVC:4 center-init from the CERTTICKETEX reply. The
// relay endpoint (SIP/SPORT) and the player-context fields are required — without
// them the agent's center-connect fails with a -39999 System Error (§14.25).
func (p agentStreamParams) centerInitData(cert map[string]any) map[string]any {
	ticket, _ := stringField(cert, "pcTicket")
	appdata, _ := stringField(cert, "pcAppendDat")
	sip, _ := intField(cert, "uiIpAddr")
	sport, _ := intField(cert, "iPort")
	return map[string]any{
		"CIP": p.CenterIP, "CPORT": asInt(p.CenterPort), "BNO": asInt(p.BNO),
		"PARENTBNO": 0, "SCRAPBJ": false, "PASSWORD": "", "GUID": p.GUID,
		"TICKETLEN": len(ticket), "TICKET": ticket, "QUALITY": 2, "APPDATA": appdata,
		"JOINLOG": p.joinlog("0"),
		"SIP":     sip, "SPORT": sport, "SPROTOCOL": 2, "BJID": p.BjID,
		"SETBPS": 8000, "SSBUF": 2, "BROWSER": "Chrome",
		"BROWSERVERSION": "150.0.7871.182", "PLAYERVERSION": "2.2.9",
	}
}

// joinlog builds the wire JOINLOG (0x11 section / 0x06 field / 0x12 end grammar).
func (p agentStreamParams) joinlog(subscribe string) string {
	uuid := p.AU
	if uuid == "" {
		uuid = p.GUID
	}
	const S = "\x06"
	kv := func(k, v string) string { return S + "&" + S + k + S + "=" + S + v }
	return "log\x11" + kv("uuid", uuid) + kv("geo_cc", "KR") + kv("geo_rc", "11") +
		kv("acpt_lang", "ko_KR") + kv("svc_lang", "ko_KR") + kv("is_iframeapi", "false") +
		kv("content_lang", "ko_KR") + kv("os", "win") + kv("is_streamer", "true") +
		kv("is_rejoin", "false") + kv("is_auto", "false") + kv("is_support_adaptive", "true") +
		kv("uuid_3rd", uuid) + kv("subscribe", subscribe) + kv("player_mode", "landing") +
		kv("sub_view_type", "non_sub") + kv("subscription_type", "basic") + "\x12" +
		"liveualog\x11" + kv("is_clearmode", "false") + kv("lowlatency", "1") +
		kv("is_streamer", "true") + kv("os", "win") + "\x12"
}
