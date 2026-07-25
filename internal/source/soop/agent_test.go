package soop

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFrameRoundtrip(t *testing.T) {
	cases := [][]byte{
		[]byte(""),
		[]byte("hi"),
		[]byte(`{"SVC":4,"RESULT":0,"DATA":{"HLS_PORT":2935}}`),
		make([]byte, 200),   // 2-byte extended length
		make([]byte, 70000), // 8-byte extended length
	}
	for i, payload := range cases {
		frame, err := encodeClientFrame(wsOpText, payload)
		if err != nil {
			t.Fatalf("case %d encode: %v", i, err)
		}
		op, got, err := readServerFrame(bufio.NewReader(strings.NewReader(string(frame))))
		if err != nil {
			t.Fatalf("case %d decode: %v", i, err)
		}
		if op != wsOpText {
			t.Errorf("case %d opcode = %d, want %d", i, op, wsOpText)
		}
		if string(got) != string(payload) {
			t.Errorf("case %d payload mismatch: got %d bytes want %d", i, len(got), len(payload))
		}
	}
}

func TestSvcOf(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{`{"SVC":4}`, 4},
		{`{"SVC":40}`, 40},
		{`{"SVC":"HTMLPORT"}`, -1},
		{`{}`, -1},
	}
	for _, c := range cases {
		var f agentFrame
		if err := json.Unmarshal([]byte(c.raw), &f); err != nil {
			t.Fatalf("unmarshal %s: %v", c.raw, err)
		}
		if got := svcOf(&f); got != c.want {
			t.Errorf("svcOf(%s) = %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestLocalPlaylistURL(t *testing.T) {
	got := localPlaylistURL("295847647", 2935)
	want := "http://127.0.0.1:2935/295847647/playlist.m3u8"
	if got != want {
		t.Errorf("localPlaylistURL = %q, want %q", got, want)
	}
}

func TestFieldHelpers(t *testing.T) {
	m := map[string]any{
		"s":     "text",
		"empty": "",
		"n":     float64(2935),
		"ns":    "1080",
	}
	if v, ok := stringField(m, "s"); !ok || v != "text" {
		t.Errorf("stringField s = %q,%v", v, ok)
	}
	if _, ok := stringField(m, "empty"); ok {
		t.Error("empty string should report absent")
	}
	if v, ok := intField(m, "n"); !ok || v != 2935 {
		t.Errorf("intField n = %d,%v", v, ok)
	}
	if v, ok := intField(m, "ns"); !ok || v != 1080 {
		t.Errorf("intField ns = %d,%v", v, ok)
	}
	if asInt("18000") != 18000 || asInt("") != 0 {
		t.Error("asInt mismatch")
	}
	if orString("", "fb") != "fb" || orString("a", "fb") != "a" {
		t.Error("orString mismatch")
	}
}

// encodeServerText builds an unmasked server text frame.
func encodeServerText(payload []byte) []byte {
	out := []byte{0x81}
	n := len(payload)
	switch {
	case n < 126:
		out = append(out, byte(n))
	case n < 65536:
		out = append(out, 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		out = append(out, ext[:]...)
	default:
		out = append(out, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		out = append(out, ext[:]...)
	}
	return append(out, payload...)
}

// mockAgent is a minimal SOOP grid agent: it upgrades the ws, then replies to
// INIT_GW with a cert ticket and to INIT_BROAD with an HLS port.
func mockAgent(t *testing.T, hlsPort int) (port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		// Read HTTP upgrade request (until blank line).
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

		for {
			op, payload, err := readServerFrame(br)
			if err != nil {
				return
			}
			if op == wsOpClose {
				return
			}
			if op != wsOpText {
				continue
			}
			var f agentFrame
			if err := json.Unmarshal(payload, &f); err != nil {
				continue
			}
			switch svcOf(&f) {
			case svcInitGW:
				resp := `{"SVC":39,"RESULT":0,"DATA":{"pcTicket":"TICKET42","pcAppendData":"APPEND42"}}`
				conn.Write(encodeServerText([]byte(resp)))
			case svcInitBroad:
				resp := `{"SVC":4,"RESULT":0,"DATA":{"HLS_PORT":` + strconv.Itoa(hlsPort) + `}}`
				conn.Write(encodeServerText([]byte(resp)))
			}
		}
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(portStr)
	return p, func() { ln.Close() }
}

func TestAgentHandshake(t *testing.T) {
	mockPort, stop := mockAgent(t, 4321)
	defer stop()

	defer func(p int) { agentServicePort = p }(agentServicePort)
	agentServicePort = mockPort

	s := New(nil)
	info := &liveInfo{
		BNO:         "295847647",
		CenterIP:    "110.10.76.232",
		CenterPort:  "18000",
		GateWayIP:   "118.218.125.112",
		GateWayPort: "3456",
		Category:    "00810000",
		FanTicket:   "ftk",
	}
	url, err := s.resolveViaAgent(context.Background(), info)
	if err != nil {
		t.Fatalf("resolveViaAgent: %v", err)
	}
	want := "http://127.0.0.1:4321/295847647/playlist.m3u8"
	if url != want {
		t.Errorf("playlist url = %q, want %q", url, want)
	}
}

func TestAgentHandshakeMissingGrid(t *testing.T) {
	s := New(nil)
	_, err := s.resolveViaAgent(context.Background(), &liveInfo{BNO: "1"})
	if err == nil {
		t.Fatal("expected error for missing grid coordinates")
	}
}

func TestResolveViaAgentTimeout(t *testing.T) {
	// Point at a dead port: dial should fail fast, not hang.
	defer func(p int, d time.Duration) { agentServicePort, agentDialTimeout = p, d }(agentServicePort, agentDialTimeout)
	agentServicePort = 1 // nothing listening
	agentDialTimeout = 500 * time.Millisecond

	s := New(nil)
	done := make(chan error, 1)
	go func() {
		_, err := s.resolveViaAgent(context.Background(), &liveInfo{
			BNO: "1", CenterIP: "1.2.3.4", GateWayIP: "1.2.3.4",
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected dial error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("resolveViaAgent hung past dial timeout")
	}
}
