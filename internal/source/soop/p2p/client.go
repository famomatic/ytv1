package p2p

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config describes one anonymous viewer session. Endpoints and FanTicket come
// from SOOP's live/watch APIs; tickets must be fetched afresh for each session.
// The receiver only opens outbound TCP connections and does not serve peers.
type Config struct {
	GatewayAddress string
	RelayAddress   string
	Broadcast      uint32
	Broadcaster    string
	Category       uint32
	GUID           string
	FanTicket      string
	JoinLog        string
	// DialContext permits custom routing and deterministic protocol tests.
	DialContext      func(context.Context, string, string) (net.Conn, error)
	HandshakeTimeout time.Duration
	IdleTimeout      time.Duration
	MaxPacketSize    int
}

type parent struct {
	IP      uint32 `json:"parent_ip"`
	Port    uint16 `json:"parent_port"`
	Quality uint32 `json:"quality"`
}

func (p parent) address() string {
	ip := net.IPv4(byte(p.IP), byte(p.IP>>8), byte(p.IP>>16), byte(p.IP>>24))
	port := p.Port>>8 | p.Port<<8
	return net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
}

func words(v ...uint32) []byte {
	b := make([]byte, len(v)*4)
	for i, w := range v {
		binary.LittleEndian.PutUint32(b[i*4:], w)
	}
	return b
}

func (c Config) validate() error {
	if c.Broadcast == 0 || c.Broadcaster == "" || len(c.Broadcaster) > 25 || strings.ContainsAny(c.Broadcaster, "\x00\x11\x12") {
		return fmt.Errorf("p2p: invalid broadcast identity")
	}
	if b, err := hex.DecodeString(c.GUID); err != nil || len(b) != 16 {
		return fmt.Errorf("p2p: GUID must be 32 hex characters")
	}
	if c.FanTicket == "" || len(c.FanTicket) > 65536 || strings.ContainsAny(c.FanTicket, "\x00\x11\x12") || strings.ContainsRune(c.JoinLog, 0) {
		return fmt.Errorf("p2p: invalid session metadata")
	}
	if c.HandshakeTimeout < 0 || c.IdleTimeout < 0 || c.MaxPacketSize < 0 {
		return fmt.Errorf("p2p: negative connection limit")
	}
	return nil
}

func (c Config) dial(ctx context.Context, address string) (net.Conn, error) {
	if c.DialContext != nil {
		return c.DialContext(ctx, "tcp", address)
	}
	return (&net.Dialer{Timeout: c.HandshakeTimeout}).DialContext(ctx, "tcp", address)
}

// Stream receives original-quality media until cancellation or an error. emit
// receives complete SOOP chunks (77-byte header followed by elementary media),
// synchronously, and may retain them. An emit error stops the session immediately.
func Stream(ctx context.Context, c Config, emit func([]byte) error) error {
	if err := c.validate(); err != nil {
		return err
	}
	if emit == nil {
		return fmt.Errorf("p2p: nil media consumer")
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = 12 * time.Second
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 15 * time.Second
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ticket, err := gatewayTicket(ctx, c)
	if err != nil {
		return fmt.Errorf("p2p: gateway: %w", err)
	}
	relay, err := c.dial(ctx, c.RelayAddress)
	if err != nil {
		return fmt.Errorf("p2p: relay connect: %w", err)
	}
	defer relay.Close()
	stopRelay := context.AfterFunc(ctx, func() { _ = relay.Close() })
	defer stopRelay()
	_ = relay.SetDeadline(time.Now().Add(c.HandshakeTimeout))
	var writeMu sync.Mutex
	send := func(op uint64, body []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = relay.SetWriteDeadline(time.Now().Add(c.HandshakeTimeout))
		return WriteRelayPacket(relay, RelayPacket{Opcode: op, Body: body}, c.MaxPacketSize)
	}
	if err = send(2, []byte{0}); err != nil {
		return err
	}
	var session uint32
	for session == 0 {
		p, e := ReadRelayPacket(relay, c.MaxPacketSize)
		if e != nil {
			return fmt.Errorf("p2p: relay session: %w", e)
		}
		if p.Opcode == 2 && len(p.Body) >= 8 {
			session = binary.LittleEndian.Uint32(p.Body[4:])
		}
	}
	join := c.JoinLog + "ticket\x11" + ticket + "\x12quality\x11ori\x12is_auto\x11false\x12passwd\x11(null)\x12\x00"
	blob := BuildAuthBlob(SessionInput(c.GUID), uint32(time.Now().Unix()), 0)
	for _, p := range []RelayPacket{
		{Opcode: 3, Body: []byte{0}},
		{Opcode: 0x3e, Body: words(0x00170004, 0)},
		{Opcode: 0x14, Body: words(1, 0, 500)},
		{Opcode: 0x2a, Body: append(words(8, uint32(len(blob))), blob...)},
		{Opcode: 0x46, Body: append(words(0, 0, 0, 20, uint32(len(join)-1)), []byte(join)...)},
	} {
		if err = send(p.Opcode, p.Body); err != nil {
			return err
		}
	}
	for {
		p, e := ReadRelayPacket(relay, c.MaxPacketSize)
		if e != nil {
			return fmt.Errorf("p2p: relay join: %w", e)
		}
		if p.Opcode == 0x46 {
			if len(p.Body) < 20 || binary.LittleEndian.Uint32(p.Body[:4]) != c.Broadcast {
				return fmt.Errorf("p2p: relay rejected broadcast")
			}
			break
		}
	}
	if err = send(0x71, jsonBody(map[string]any{"broad_no": 0, "relay_port": 0, "reverse_port": 0})); err != nil {
		return err
	}
	if err = send(0x50, words(c.Broadcast)); err != nil {
		return err
	}
	allocate := func() error {
		return send(0x69, jsonBody(map[string]any{"list": []map[string]uint32{{"alloc_type": 1, "parent_ip": 0, "parent_port": 0, "quality": 1}}}))
	}
	if err = allocate(); err != nil {
		return err
	}
	parents := make(chan []parent, 1)
	relayError := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_ = relay.SetReadDeadline(time.Now().Add(6 * time.Minute))
			p, e := ReadRelayPacket(relay, c.MaxPacketSize)
			if e != nil {
				relayError <- e
				cancel()
				return
			}
			if p.Opcode == 1 {
				if e = send(1, p.Body); e != nil {
					relayError <- e
					cancel()
					return
				}
			}
			if p.Opcode != 0x69 {
				continue
			}
			var list struct {
				List []parent `json:"list"`
			}
			if e = decodeJSONBody(p.Body, &list); e != nil {
				relayError <- e
				cancel()
				return
			}
			select {
			case parents <- list.List:
			default:
			}
		}
	}()
	defer func() { cancel(); _ = relay.Close(); <-done }()
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		timer := time.NewTimer(c.HandshakeTimeout)
		var list []parent
		select {
		case list = <-parents:
			timer.Stop()
		case <-ctx.Done():
			timer.Stop()
			select {
			case e := <-relayError:
				return fmt.Errorf("p2p: relay: %w", e)
			default:
				return ctx.Err()
			}
		case <-timer.C:
			return fmt.Errorf("p2p: parent allocation timed out")
		}
		for _, p := range list {
			if p.Quality != 1 || p.IP == 0 || p.Port == 0 {
				continue
			}
			lastErr = receivePeer(ctx, c, p, session, send, emit)
			var consumer *consumerError
			if errors.As(lastErr, &consumer) {
				return consumer.err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		if err = allocate(); err != nil {
			return err
		}
	}
	return fmt.Errorf("p2p: no usable parent: %w", lastErr)
}

func gatewayTicket(ctx context.Context, c Config) (string, error) {
	conn, err := c.dial(ctx, c.GatewayAddress)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(c.HandshakeTimeout))
	add := "ad_lang\x11ko\x12is_auto\x110\x12"
	login := append([]byte{42, 0, 2, 0}, []byte(c.GUID)...)
	login = append(login, 0)
	login = append(login, words(uint32(len(add)))...)
	login = append(login, []byte(add)...)
	if err = WriteGatewayPacket(conn, GatewayPacket{Opcode: 0x3e2, Body: login}, c.MaxPacketSize); err != nil {
		return "", err
	}
	for {
		p, e := ReadGatewayPacket(conn, c.MaxPacketSize)
		if e != nil {
			return "", e
		}
		if p.Result != 0 {
			return "", fmt.Errorf("login result %d", p.Result)
		}
		if p.Opcode == 0x3e2 {
			break
		}
	}
	body := make([]byte, 71)
	binary.LittleEndian.PutUint32(body, c.Broadcast)
	binary.LittleEndian.PutUint32(body[4:], 1)
	copy(body[33:59], c.Broadcaster)
	binary.LittleEndian.PutUint32(body[59:], 42)
	binary.LittleEndian.PutUint32(body[63:], c.Category)
	binary.LittleEndian.PutUint32(body[67:], uint32(len(c.FanTicket)))
	body = append(body, c.FanTicket...)
	if err = WriteGatewayPacket(conn, GatewayPacket{Opcode: 0x415, Body: body}, c.MaxPacketSize); err != nil {
		return "", err
	}
	for {
		p, e := ReadGatewayPacket(conn, c.MaxPacketSize)
		if e != nil {
			return "", e
		}
		if p.Result != 0 {
			return "", fmt.Errorf("ticket result %d", p.Result)
		}
		if p.Opcode != 0x415 {
			continue
		}
		if len(p.Body) < 16 || binary.LittleEndian.Uint32(p.Body) != c.Broadcast || uint64(binary.LittleEndian.Uint32(p.Body[12:])) != uint64(len(p.Body)-16) {
			return "", fmt.Errorf("invalid ticket envelope")
		}
		ticket, _, ok := bytes.Cut(p.Body[16:], []byte{0})
		if !ok || len(ticket) == 0 || bytes.ContainsAny(ticket, "\x11\x12") {
			return "", fmt.Errorf("invalid gateway ticket")
		}
		return string(ticket), nil
	}
}

func jsonBody(v any) []byte {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	return append(words(uint32(len(b))), b...)
}

func decodeJSONBody(b []byte, v any) error {
	if len(b) < 4 {
		return fmt.Errorf("p2p: missing JSON length")
	}
	n := uint64(binary.LittleEndian.Uint32(b))
	if n > uint64(len(b)-4) {
		return fmt.Errorf("p2p: truncated JSON body")
	}
	return json.Unmarshal(b[4:4+int(n)], v)
}

type consumerError struct{ err error }

func (e *consumerError) Error() string { return e.err.Error() }

func receivePeer(ctx context.Context, c Config, p parent, session uint32, send func(uint64, []byte) error, emit func([]byte) error) error {
	conn, err := c.dial(ctx, p.address())
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(c.HandshakeTimeout))
	for _, packet := range []PeerPacket{
		{Opcode: 0xcb2e, Body: words(c.Broadcast, session, 1)},
		{Opcode: 0xcb2f, Body: words(1)},
		{Opcode: 0xcb30, Body: words(1, 0, 0, 2)},
	} {
		if err = WritePeerPacket(conn, packet, c.MaxPacketSize); err != nil {
			return err
		}
	}
	if err = send(0x6c, jsonBody(map[string]uint32{"quality": 1, "parent_ip": p.IP, "is_reverse": 0})); err != nil {
		return err
	}
	for {
		_ = conn.SetReadDeadline(time.Now().Add(c.IdleTimeout))
		packet, e := ReadPeerPacket(conn, c.MaxPacketSize)
		if e != nil {
			return e
		}
		if packet.Opcode != 0xc748 {
			continue
		}
		b := packet.Body
		if len(b) < 69 || binary.LittleEndian.Uint32(b[:4]) != 0x450001 || uint64(binary.LittleEndian.Uint32(b[8:])) != uint64(len(b)-69) {
			return fmt.Errorf("p2p: invalid media chunk")
		}
		chunk := make([]byte, 8+len(b))
		for i := 0; i < 8; i++ {
			chunk[i] = 255
		}
		copy(chunk[8:], b)
		if e = emit(chunk); e != nil {
			return &consumerError{e}
		}
	}
}
