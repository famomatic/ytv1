package soop

// Minimal RFC 6455 WebSocket client, just enough to talk to the SOOP grid
// agent's local control socket (ws://127.0.0.1:<port>/Websocket). The agent
// speaks plain ws over loopback (no TLS), exchanging small single-frame JSON
// text messages, so this intentionally omits fragmentation, compression, and
// the wss/TLS path that a general-purpose library would carry.

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	wsOpText   = 0x1
	wsOpClose  = 0x8
	wsOpPing   = 0x9
	wsOpPong   = 0xA
	wsMaxFrame = 1 << 20 // 1 MiB: agent control frames are tiny; cap defensively.
)

// wsConn is a loopback text-only WebSocket connection.
type wsConn struct {
	conn    net.Conn
	writeMu sync.Mutex
	br      *bufio.Reader
}

// wsDial opens a WebSocket connection to host (host:port) at the given path.
func wsDial(host, path string, timeout time.Duration) (*wsConn, error) {
	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)

	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: http://" + host + "\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, " 101") {
		conn.Close()
		return nil, fmt.Errorf("soop: agent ws upgrade failed: %s", strings.TrimSpace(status))
	}
	// Drain the remaining response headers.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return &wsConn{conn: conn, br: br}, nil
}

// writeText sends a masked text frame (clients MUST mask, per RFC 6455).
func (c *wsConn) writeText(payload []byte) error {
	return c.writeFrame(wsOpText, payload)
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(agentDialTimeout))
	frame, err := encodeClientFrame(opcode, payload)
	if err != nil {
		return err
	}
	_, err = c.conn.Write(frame)
	return err
}

// readText reads frames until a text (or continuation-completing text) message
// arrives, transparently answering pings and surfacing close frames as EOF.
func (c *wsConn) readText() ([]byte, error) {
	for {
		opcode, payload, err := readServerFrame(c.br)
		if err != nil {
			return nil, err
		}
		switch opcode {
		case wsOpText:
			return payload, nil
		case wsOpPing:
			if err := c.writeFrame(wsOpPong, payload); err != nil {
				return nil, err
			}
		case wsOpClose:
			return nil, io.EOF
		default:
			// Ignore pong / unexpected opcodes and keep reading.
		}
	}
}

const wsOpBinary = 0x2

// readAny reads the next data message, returning its opcode (text or binary) and
// payload. It answers pings and surfaces close as EOF. Used by the agent-stream
// path, which interleaves JSON control frames (text) with binary media frames.
func (c *wsConn) readAny() (byte, []byte, error) {
	for {
		opcode, payload, err := readServerFrame(c.br)
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case wsOpText, wsOpBinary:
			return opcode, payload, nil
		case wsOpPing:
			if err := c.writeFrame(wsOpPong, payload); err != nil {
				return 0, nil, err
			}
		case wsOpClose:
			return 0, nil, io.EOF
		default:
			// Ignore pong / unexpected opcodes and keep reading.
		}
	}
}

func (c *wsConn) close() error {
	_ = c.writeFrame(wsOpClose, nil)
	return c.conn.Close()
}

// encodeClientFrame builds a single masked client frame (FIN set).
func encodeClientFrame(opcode byte, payload []byte) ([]byte, error) {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return nil, err
	}

	n := len(payload)
	var header []byte
	header = append(header, 0x80|opcode) // FIN + opcode
	switch {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n < 65536:
		header = append(header, 0x80|126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		header = append(header, ext[:]...)
	default:
		header = append(header, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}
	header = append(header, mask[:]...)

	out := make([]byte, 0, len(header)+n)
	out = append(out, header...)
	for i := range n {
		out = append(out, payload[i]^mask[i&3])
	}
	return out, nil
}

// readServerFrame reads one unmasked server frame. Server frames are never
// masked; a masked server frame is a protocol violation and rejected.
func readServerFrame(br *bufio.Reader) (opcode byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(br, h[:]); err != nil {
		return 0, nil, err
	}
	opcode = h[0] & 0x0F
	masked := h[1]&0x80 != 0
	length := uint64(h[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > wsMaxFrame {
		return 0, nil, fmt.Errorf("soop: agent ws frame too large: %d", length)
	}

	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(br, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(br, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return opcode, payload, nil
}
