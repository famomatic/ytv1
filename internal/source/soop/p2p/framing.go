package p2p

import (
	"encoding/binary"
	"fmt"
	"io"
)

// DefaultMaxPacketSize bounds allocation from untrusted packet lengths. Readers
// accept an explicit limit so a caller can choose a different media budget.
const DefaultMaxPacketSize = 8 << 20

// GatewayPacket is the 16-byte gateway envelope from the captures described in
// docs/SOOP_P2P_GRID_ANALYSIS.md. Tag is the opcode XOR the body length,
// validated on reads and computed on writes. It is not a cryptographic MAC.
type GatewayPacket struct {
	Opcode uint32
	Result int32
	Tag    uint32
	Body   []byte
}

// RelayPacket is the ISS-direct envelope from section 14.21: an eight-byte
// opcode, eight-byte body length, four-byte tag, then body. It is distinct from
// both the gateway envelope and the older 0x9c4x ISS inner header.
type RelayPacket struct {
	Opcode uint64
	Tag    uint32
	Body   []byte
}

// ReadGatewayPacket reads exactly one packet, including across TCP read
// boundaries. A zero limit uses DefaultMaxPacketSize; a negative limit is invalid.
// A partial header/body returns io.ErrUnexpectedEOF. After an error, callers
// must discard the connection: the next packet boundary is not known.
func ReadGatewayPacket(r io.Reader, maxBody int) (GatewayPacket, error) {
	var packet GatewayPacket
	limit, err := packetLimit(maxBody)
	if err != nil {
		return packet, err
	}
	var header [16]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return packet, fmt.Errorf("p2p: gateway header: %w", err)
	}
	length := uint64(binary.LittleEndian.Uint32(header[8:12]))
	if binary.LittleEndian.Uint32(header[12:]) != binary.LittleEndian.Uint32(header[:4])^uint32(length) {
		return packet, fmt.Errorf("p2p: gateway checksum mismatch")
	}
	body, err := readPacketBody(r, length, limit)
	if err != nil {
		return packet, fmt.Errorf("p2p: gateway body: %w", err)
	}
	return GatewayPacket{
		Opcode: binary.LittleEndian.Uint32(header[:4]),
		Result: int32(binary.LittleEndian.Uint32(header[4:8])),
		Tag:    binary.LittleEndian.Uint32(header[12:16]),
		Body:   body,
	}, nil
}

// WriteGatewayPacket writes a complete gateway envelope and body. Writes on a
// shared connection must be serialized by the caller. On error, discard the
// connection rather than retrying a partially written packet.
func WriteGatewayPacket(w io.Writer, packet GatewayPacket, maxBody int) error {
	if err := checkPacketSize(len(packet.Body), maxBody); err != nil {
		return err
	}
	if uint64(len(packet.Body)) > uint64(^uint32(0)) {
		return fmt.Errorf("p2p: gateway body exceeds uint32 length")
	}
	var header [16]byte
	binary.LittleEndian.PutUint32(header[:4], packet.Opcode)
	binary.LittleEndian.PutUint32(header[4:8], uint32(packet.Result))
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(packet.Body)))
	binary.LittleEndian.PutUint32(header[12:16], packet.Opcode^uint32(len(packet.Body)))
	if err := writePacketBytes(w, header[:]); err != nil {
		return fmt.Errorf("p2p: gateway header: %w", err)
	}
	if err := writePacketBytes(w, packet.Body); err != nil {
		return fmt.Errorf("p2p: gateway body: %w", err)
	}
	return nil
}

// ReadRelayPacket reads one ISS-direct packet. Limit and error semantics are
// the same as ReadGatewayPacket.
func ReadRelayPacket(r io.Reader, maxBody int) (RelayPacket, error) {
	var packet RelayPacket
	limit, err := packetLimit(maxBody)
	if err != nil {
		return packet, err
	}
	var header [20]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return packet, fmt.Errorf("p2p: relay header: %w", err)
	}
	length := binary.LittleEndian.Uint64(header[8:16])
	if binary.LittleEndian.Uint32(header[16:]) != binary.LittleEndian.Uint32(header[:4])^uint32(length) {
		return packet, fmt.Errorf("p2p: relay checksum mismatch")
	}
	body, err := readPacketBody(r, length, limit)
	if err != nil {
		return packet, fmt.Errorf("p2p: relay body: %w", err)
	}
	return RelayPacket{
		Opcode: binary.LittleEndian.Uint64(header[:8]),
		Tag:    binary.LittleEndian.Uint32(header[16:20]),
		Body:   body,
	}, nil
}

// WriteRelayPacket writes one ISS-direct packet. Connection ownership and
// error semantics are the same as WriteGatewayPacket.
func WriteRelayPacket(w io.Writer, packet RelayPacket, maxBody int) error {
	if err := checkPacketSize(len(packet.Body), maxBody); err != nil {
		return err
	}
	var header [20]byte
	binary.LittleEndian.PutUint64(header[:8], packet.Opcode)
	binary.LittleEndian.PutUint64(header[8:16], uint64(len(packet.Body)))
	binary.LittleEndian.PutUint32(header[16:20], uint32(packet.Opcode)^uint32(len(packet.Body)))
	if err := writePacketBytes(w, header[:]); err != nil {
		return fmt.Errorf("p2p: relay header: %w", err)
	}
	if err := writePacketBytes(w, packet.Body); err != nil {
		return fmt.Errorf("p2p: relay body: %w", err)
	}
	return nil
}

// PeerPacket is a grid parent's packet. The final header word is reserved;
// captures contain uninitialized client memory there, so writers zero it.
type PeerPacket struct {
	Opcode uint16
	Body   []byte
}

func ReadPeerPacket(r io.Reader, maxBody int) (PeerPacket, error) {
	var p PeerPacket
	limit, err := packetLimit(maxBody)
	if err != nil {
		return p, err
	}
	var h [16]byte
	if _, err = io.ReadFull(r, h[:]); err != nil {
		return p, fmt.Errorf("p2p: peer header: %w", err)
	}
	op := binary.LittleEndian.Uint16(h[2:])
	n := binary.LittleEndian.Uint32(h[4:])
	if binary.LittleEndian.Uint16(h[:]) != 0x202 || binary.LittleEndian.Uint32(h[8:]) != 0x202^uint32(op)^n {
		return p, fmt.Errorf("p2p: invalid peer header")
	}
	body, err := readPacketBody(r, uint64(n), limit)
	if err != nil {
		return p, fmt.Errorf("p2p: peer body: %w", err)
	}
	return PeerPacket{Opcode: op, Body: body}, nil
}

func WritePeerPacket(w io.Writer, p PeerPacket, maxBody int) error {
	if err := checkPacketSize(len(p.Body), maxBody); err != nil {
		return err
	}
	if uint64(len(p.Body)) > uint64(^uint32(0)) {
		return fmt.Errorf("p2p: peer body exceeds uint32 length")
	}
	var h [16]byte
	binary.LittleEndian.PutUint16(h[:], 0x202)
	binary.LittleEndian.PutUint16(h[2:], p.Opcode)
	binary.LittleEndian.PutUint32(h[4:], uint32(len(p.Body)))
	binary.LittleEndian.PutUint32(h[8:], 0x202^uint32(p.Opcode)^uint32(len(p.Body)))
	if err := writePacketBytes(w, h[:]); err != nil {
		return err
	}
	return writePacketBytes(w, p.Body)
}

func packetLimit(limit int) (uint64, error) {
	if limit < 0 {
		return 0, fmt.Errorf("p2p: negative packet size limit")
	}
	if limit == 0 {
		limit = DefaultMaxPacketSize
	}
	return uint64(limit), nil
}

func checkPacketSize(size, maxBody int) error {
	limit, err := packetLimit(maxBody)
	if err != nil {
		return err
	}
	if uint64(size) > limit {
		return fmt.Errorf("p2p: packet body length %d exceeds limit %d", size, limit)
	}
	return nil
}

func readPacketBody(r io.Reader, length, limit uint64) ([]byte, error) {
	if length > limit {
		return nil, fmt.Errorf("p2p: packet body length %d exceeds limit %d", length, limit)
	}
	body := make([]byte, int(length)) // limit originated as an int
	if _, err := io.ReadFull(r, body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF // a header already declared a nonempty body
		}
		return nil, err
	}
	return body, nil
}

func writePacketBytes(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return io.ErrShortWrite
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
