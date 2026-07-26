package soop

// SOOP P2P media deframer. The local agent pushes the live stream as binary
// WebSocket frames carrying a sequence of length-delimited chunks. Each chunk is
// a 77-byte little-endian header followed by the payload:
//
//	off  size  field
//	 0    8    magic FF*8
//	 8    4    0x00450001 (0x45 = 69 = header bytes after this field; 8+69=77)
//	16    4    payloadLen (u32)
//	...        seq / per-stream counters / timestamps (not needed to demux)
//
// Chunks are parsed sequentially (next = pos + 77 + payloadLen); the magic must
// NOT be scanned for, since 8x0xFF also occurs inside payloads. Each payload is a
// standard elementary-stream unit, classified by prefix:
//   - H.264 Annex-B video: starts 00 00 00 01
//   - ADTS AAC audio:      starts FF Fx
//
// (Verified live: h264 High 1920x1080 + aac 48000 2ch. See §14.25 of the analysis.)

import "encoding/binary"

const chunkHeaderLen = 77

// chunkKind classifies a deframed payload.
type chunkKind int

const (
	chunkOther chunkKind = iota
	chunkVideo
	chunkAudio
)

// classifyPayload identifies a chunk payload by its leading bytes.
func classifyPayload(p []byte) chunkKind {
	if len(p) >= 4 && p[0] == 0 && p[1] == 0 && p[2] == 0 && p[3] == 1 {
		return chunkVideo
	}
	if len(p) >= 2 && p[0] == 0xFF && p[1]&0xF0 == 0xF0 {
		return chunkAudio
	}
	return chunkOther
}

// deframer accumulates binary frame bytes and yields complete chunks. It holds a
// carry buffer for chunks split across WebSocket frames.
type deframer struct {
	buf []byte
}

// push appends raw binary-frame bytes and invokes emit for each complete chunk,
// in order. Incomplete trailing bytes are retained for the next push.
func (d *deframer) push(b []byte, emit func(kind chunkKind, payload []byte)) {
	d.buf = append(d.buf, b...)
	pos := 0
	for pos+chunkHeaderLen <= len(d.buf) {
		// Header must start with the 8xFF magic. If it doesn't, we are
		// mid-stream out of sync; advance one byte to resynchronize.
		if !hasChunkMagic(d.buf[pos:]) {
			pos++
			continue
		}
		payloadLen := int(binary.LittleEndian.Uint32(d.buf[pos+16:]))
		if payloadLen < 0 || pos+chunkHeaderLen+payloadLen > len(d.buf) {
			break // wait for more bytes
		}
		payload := d.buf[pos+chunkHeaderLen : pos+chunkHeaderLen+payloadLen]
		emit(classifyPayload(payload), payload)
		pos += chunkHeaderLen + payloadLen
	}
	// Retain the unconsumed tail.
	if pos > 0 {
		d.buf = append(d.buf[:0], d.buf[pos:]...)
	}
}

func hasChunkMagic(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	for i := range 8 {
		if b[i] != 0xFF {
			return false
		}
	}
	return true
}
