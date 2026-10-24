package p2p

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/iotest"
)

func TestGatewayFrameCaptureLayout(t *testing.T) {
	// Section 14.16: 78-byte LOGIN, including the NUL before addinfo.
	body := append(mustHex("2a000200"), []byte(capturedGUID)...)
	body = append(body, mustHex("0015000000")...)
	body = append(body, []byte("ad_lang\x11ko\x12is_auto\x110\x12")...)
	want := append(mustHex("e2030000000000003e000000dc030000"), body...)
	var wire bytes.Buffer
	if err := WriteGatewayPacket(&wire, GatewayPacket{Opcode: 0x3e2, Tag: 0x3dc, Body: body}, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire.Bytes(), want) || len(want) != 78 {
		t.Fatalf("gateway wire = %x (len=%d)", wire.Bytes(), wire.Len())
	}
	packet, err := ReadGatewayPacket(iotest.OneByteReader(&wire), 0)
	if err != nil || packet.Opcode != 0x3e2 || packet.Tag != 0x3dc || packet.Result != 0 || !bytes.Equal(packet.Body, body) {
		t.Fatalf("read gateway = %+v, %v", packet, err)
	}
}

func TestRelayFrameCaptureLayout(t *testing.T) {
	// Section 14.21: opcode 2, 64-bit body length of 1, tag 0, body 00.
	want := mustHex("020000000000000001000000000000000300000000")
	var wire bytes.Buffer
	if err := WriteRelayPacket(&wire, RelayPacket{Opcode: 2, Body: []byte{0}}, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire.Bytes(), want) {
		t.Fatalf("relay wire = %x, want %x", wire.Bytes(), want)
	}
	packet, err := ReadRelayPacket(iotest.OneByteReader(&wire), 0)
	if err != nil || packet.Opcode != 2 || packet.Tag != 3 || !bytes.Equal(packet.Body, []byte{0}) {
		t.Fatalf("read relay = %+v, %v", packet, err)
	}
}

func TestFrameBoundaries(t *testing.T) {
	for _, gateway := range []bool{true, false} {
		var wire bytes.Buffer
		for _, body := range [][]byte{nil, {1, 2, 3}, {255, 255, 255, 255, 0, 1}} {
			var err error
			if gateway {
				err = WriteGatewayPacket(&wire, GatewayPacket{Opcode: 44, Result: -999, Tag: 123, Body: body}, 0)
			} else {
				err = WriteRelayPacket(&wire, RelayPacket{Opcode: 44, Tag: 123, Body: body}, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, want := range [][]byte{nil, {1, 2, 3}, {255, 255, 255, 255, 0, 1}} {
			var body []byte
			if gateway {
				p, err := ReadGatewayPacket(&wire, 6)
				if err != nil || p.Result != -999 || p.Opcode != 44 || p.Tag != 44^uint32(len(want)) {
					t.Fatalf("gateway: %+v, %v", p, err)
				}
				body = p.Body
			} else {
				p, err := ReadRelayPacket(&wire, 6)
				if err != nil || p.Opcode != 44 || p.Tag != 44^uint32(len(want)) {
					t.Fatalf("relay: %+v, %v", p, err)
				}
				body = p.Body
			}
			if !bytes.Equal(body, want) {
				t.Fatalf("body = %x, want %x", body, want)
			}
		}
		if wire.Len() != 0 {
			t.Fatal("packet bytes left unread")
		}
	}
}

func TestFrameTruncation(t *testing.T) {
	for _, gateway := range []bool{true, false} {
		var wire bytes.Buffer
		if gateway {
			if err := WriteGatewayPacket(&wire, GatewayPacket{Body: []byte{1, 2, 3}}, 0); err != nil {
				t.Fatal(err)
			}
		} else if err := WriteRelayPacket(&wire, RelayPacket{Body: []byte{1, 2, 3}}, 0); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < wire.Len(); i++ {
			r := bytes.NewReader(wire.Bytes()[:i])
			var err error
			if gateway {
				_, err = ReadGatewayPacket(r, 0)
			} else {
				_, err = ReadRelayPacket(r, 0)
			}
			want := io.ErrUnexpectedEOF
			if i == 0 {
				want = io.EOF
			}
			if !errors.Is(err, want) {
				t.Fatalf("gateway=%v truncated length=%d: %v, want %v", gateway, i, err, want)
			}
		}
	}
}

func TestFrameLengthLimits(t *testing.T) {
	var gateway [16]byte
	binary.LittleEndian.PutUint32(gateway[8:12], ^uint32(0))
	if _, err := ReadGatewayPacket(bytes.NewReader(gateway[:]), 0); err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("gateway did not reject oversized length before reading: %v", err)
	}
	var relay [20]byte
	for _, length := range []uint64{DefaultMaxPacketSize + 1, 1 << 32, 1 << 63, ^uint64(0)} {
		binary.LittleEndian.PutUint64(relay[8:16], length)
		if _, err := ReadRelayPacket(bytes.NewReader(relay[:]), 0); err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("relay did not reject oversized length %d before reading: %v", length, err)
		}
	}
	for _, limit := range []int{-1, 1} {
		var wire bytes.Buffer
		if err := WriteGatewayPacket(&wire, GatewayPacket{Body: []byte{1, 2}}, limit); err == nil || wire.Len() != 0 {
			t.Fatalf("gateway wrote invalid packet with limit %d", limit)
		}
		if err := WriteRelayPacket(&wire, RelayPacket{Body: []byte{1, 2}}, limit); err == nil || wire.Len() != 0 {
			t.Fatalf("relay wrote invalid packet with limit %d", limit)
		}
	}
}

type packetTestWriter struct {
	bytes.Buffer
	chunk int
	err   error
}

func (w *packetTestWriter) Write(b []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	return w.Buffer.Write(b[:min(len(b), w.chunk)])
}

func TestFrameShortWrites(t *testing.T) {
	for _, write := range []func(io.Writer) error{
		func(w io.Writer) error { return WriteGatewayPacket(w, GatewayPacket{Body: []byte{1, 2, 3}}, 0) },
		func(w io.Writer) error { return WriteRelayPacket(w, RelayPacket{Body: []byte{1, 2, 3}}, 0) },
	} {
		var want bytes.Buffer
		if err := write(&want); err != nil {
			t.Fatal(err)
		}
		short := &packetTestWriter{chunk: 1}
		if err := write(short); err != nil || !bytes.Equal(short.Bytes(), want.Bytes()) {
			t.Fatalf("partial write: %v", err)
		}
		if err := write(&packetTestWriter{}); !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("zero-progress write: %v", err)
		}
		if err := write(&packetTestWriter{err: io.ErrClosedPipe}); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("write error: %v", err)
		}
	}
}

func FuzzReadRelayPacket(f *testing.F) {
	f.Add(mustHex("020000000000000001000000000000000300000000"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, wire []byte) {
		packet, err := ReadRelayPacket(bytes.NewReader(wire), 4096)
		if err != nil {
			return
		}
		var rebuilt bytes.Buffer
		if err := WriteRelayPacket(&rebuilt, packet, 4096); err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(wire, rebuilt.Bytes()) {
			t.Fatal("packet failed to preserve wire bytes")
		}
	})
}
