package soop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// mockLiveAgent stands in for the local SOOP grid agent: it speaks the live
// streaming choreography (CAPTION→HTMLPORT, then INIT_GW→CERTTICKETEX→ADDINFO→
// START→binary media) over loopback so the whole path can be exercised without a
// real SOOPStreamer install or a live broadcast. It returns the port to point
// agentServicePort at and a stop func.
func mockLiveAgent(t *testing.T) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveMockAgentConn(conn, port)
		}
	}()
	return port, func() { _ = ln.Close() }
}

func serveMockAgentConn(conn net.Conn, selfPort int) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	reqLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	path := ""
	if f := strings.Fields(reqLine); len(f) >= 2 {
		path = f[1]
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")

	// Control socket: hand back the (same) session port via HTMLPORT.
	if path == agentWSPath {
		if _, _, err := readServerFrame(br); err != nil {
			return
		}
		resp := fmt.Sprintf(`{"SVC":"HTMLPORT","DATA":{"HTMLPLAYER_PORT":%d}}`, selfPort)
		conn.Write(encodeServerFrame(wsOpText, []byte(resp)))
		return
	}

	// Session socket: run the choreography, then stream media and close.
	sentAddInfo := false
	for {
		op, payload, err := readServerFrame(br)
		if err != nil {
			return
		}
		if op != wsOpText {
			continue
		}
		var m struct {
			SVC int `json:"SVC"`
		}
		_ = json.Unmarshal(payload, &m)
		switch m.SVC {
		case svcInitGW: // 40
			conn.Write(encodeServerFrame(wsOpText, []byte(`{"SVC":39,"DATA":{"pcTicket":"T","pcAppendDat":"A","uiIpAddr":123456,"iPort":18000}}`)))
		case svcUID: // 30, sent right after the client's INIT_BROAD
			if !sentAddInfo {
				sentAddInfo = true
				conn.Write(encodeServerFrame(wsOpText, []byte(`{"SVC":4,"DATA":{"ADDINFO":"preset"}}`)))
			}
		case svcStart: // 5
			for _, fr := range mockMediaFrames() {
				if _, err := conn.Write(encodeServerFrame(wsOpBinary, fr)); err != nil {
					return
				}
			}
			// Signal end-of-stream with a proper close frame, then keep reading
			// until the client disconnects. Returning here would close the TCP
			// socket abruptly (RST), which can drop still-buffered media frames
			// before the client reads them.
			conn.Write(encodeServerFrame(wsOpClose, nil))
			for {
				if _, _, err := readServerFrame(br); err != nil {
					return
				}
			}
		}
	}
}

// encodeServerFrame builds an unmasked server frame (FIN set).
func encodeServerFrame(opcode byte, payload []byte) []byte {
	out := []byte{0x80 | opcode}
	n := len(payload)
	switch {
	case n < 126:
		out = append(out, byte(n))
	case n < 65536:
		out = append(out, 126)
		var e [2]byte
		binary.BigEndian.PutUint16(e[:], uint16(n))
		out = append(out, e[:]...)
	default:
		out = append(out, 127)
		var e [8]byte
		binary.BigEndian.PutUint64(e[:], uint64(n))
		out = append(out, e[:]...)
	}
	return append(out, payload...)
}

func chunkFrame(payload []byte) []byte {
	h := make([]byte, chunkHeaderLen)
	for i := range 8 {
		h[i] = 0xFF
	}
	binary.LittleEndian.PutUint32(h[8:], 0x00450001)
	binary.LittleEndian.PutUint32(h[16:], uint32(len(payload)))
	return append(h, payload...)
}

func mockMediaFrames() [][]byte {
	sps := append([]byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x2A}, bytes.Repeat([]byte{0xAB}, 120)...)
	idr := append([]byte{0x00, 0x00, 0x00, 0x01, 0x65}, bytes.Repeat([]byte{0xCD}, 4096)...)
	return [][]byte{chunkFrame(sps), chunkFrame(mockADTS(96)), chunkFrame(idr), chunkFrame(mockADTS(96))}
}

func mockADTS(n int) []byte {
	if n < 7 {
		n = 7
	}
	f := make([]byte, n)
	f[0], f[1] = 0xFF, 0xF1
	f[3] = byte((n >> 11) & 0x03)
	f[4] = byte(n >> 3)
	f[5] = byte((n&0x07)<<5) | 0x1F
	return f
}

// assertTSHasVideoAudio checks the bytes are whole 188-byte TS packets carrying
// PAT, PMT, video and audio PIDs.
func assertTSHasVideoAudio(t *testing.T, out []byte) {
	t.Helper()
	if len(out) == 0 || len(out)%tsLen != 0 {
		t.Fatalf("output not a whole number of 188-byte TS packets: %d", len(out))
	}
	var sawPAT, sawPMT, sawVideo, sawAudio bool
	for off := 0; off < len(out); off += tsLen {
		pkt := out[off : off+tsLen]
		if pkt[0] != tsSync {
			t.Fatalf("packet at %d missing sync 0x47", off)
		}
		switch (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2]) {
		case 0x0000:
			sawPAT = true
		case tsPMT:
			sawPMT = true
		case tsVideo:
			sawVideo = true
		case tsAudio:
			sawAudio = true
		}
	}
	if !sawPAT || !sawPMT || !sawVideo || !sawAudio {
		t.Fatalf("missing PID: PAT=%v PMT=%v video=%v audio=%v", sawPAT, sawPMT, sawVideo, sawAudio)
	}
}

func TestStreamAgentMediaChoreography(t *testing.T) {
	port, stop := mockLiveAgent(t)
	defer stop()
	defer func(p int) { agentServicePort = p }(agentServicePort)
	agentServicePort = port

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var buf bytes.Buffer
	// The mock closes the session after streaming, so streamAgentMedia returns
	// io.EOF with media already muxed — that is a normal end of stream.
	_ = New(nil).streamAgentMedia(ctx, agentStreamParams{BNO: "123", BjID: "bj", GUID: "G", FanTicket: "ft"}, &buf)
	assertTSHasVideoAudio(t, buf.Bytes())
}

func TestAgentHandlerRangeFallbackAndStream(t *testing.T) {
	port, stop := mockLiveAgent(t)
	defer stop()
	defer func(p int) { agentServicePort = p }(agentServicePort)
	agentServicePort = port

	srv := httptest.NewServer(New(nil).agentHandler(agentStreamParams{BNO: "1", BjID: "bj"}, nil))
	defer srv.Close()

	// A *bounded* Range probe (bytes=A-B, as ytv1's chunked downloader sends) must
	// get 200 / Accept-Ranges:none with no media, so the downloader falls back to a
	// single plain GET without consuming the one-shot stream.
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("range probe: %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Accept-Ranges") != "none" {
		t.Fatalf("range probe status=%d accept-ranges=%q", resp.StatusCode, resp.Header.Get("Accept-Ranges"))
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) != 0 {
		t.Fatalf("bounded range probe should not stream media, got %d bytes", len(body))
	}

	// An *open* Range (bytes=A-, as MPV/ffmpeg sends to test seekability) streams
	// the muxed MPEG-TS — returning an empty 200 here made the player stall.
	req2, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req2.Header.Set("Range", "bytes=0-")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("open range GET: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	assertTSHasVideoAudio(t, body2)
}

// TestServeAgentStreamInProcessByDefault verifies a download run (detachServe
// false) serves the stream in-process — no detached `ytv1 soopserve` child that
// could outlive a wedged consumer — and that the returned URL actually streams
// the muxed MPEG-TS.
func TestServeAgentStreamInProcessByDefault(t *testing.T) {
	port, stop := mockLiveAgent(t)
	defer stop()
	defer func(p int) { agentServicePort = p }(agentServicePort)
	agentServicePort = port

	defer func(v bool) { detachServe = v }(detachServe)
	detachServe = false // a download run
	defer func() { agentStreamByBNO = map[string]string{} }()
	agentStreamByBNO = map[string]string{}

	u, err := New(nil).serveAgentStream(agentStreamParams{BNO: "42", BjID: "bj"})
	if err != nil {
		t.Fatalf("serveAgentStream: %v", err)
	}
	if !strings.HasPrefix(u, "http://127.0.0.1:") {
		t.Fatalf("expected loopback url, got %q", u)
	}
	// The cache returns the same URL on a second call (no second agent session).
	if u2, _ := New(nil).serveAgentStream(agentStreamParams{BNO: "42", BjID: "bj"}); u2 != u {
		t.Fatalf("cache miss: %q != %q", u2, u)
	}
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assertTSHasVideoAudio(t, body)
}
