package p2p

import "encoding/binary"

// Protocol map reconstructed from NetControl.dll (Ghidra decompile). The engine
// speaks THREE distinct binary protocols over three TCP connections:
//
//  1. Gateway  (GWIP:GWPT, e.g. 218.38.31.x:3456) — auth/login; returns a cert
//     ticket and an AES-encrypted "recommend/enckey" blob (session key material,
//     decrypted by CNetControlObject::_RecommendEncKeyData with the static AES
//     key in aes.go).
//  2. Center   (CTIP:CTPT, e.g. 110.10.76.x:18000) — the getnode coordinator.
//     Plaintext, 8-byte little-endian header fields (observed on the wire:
//     4a.. 34.. 7e.. <ip> …). Returns parent/ISS node coordinates + quality/CDN
//     ({"quality":%d,"parent_ip":%u,"is_reverse":%d}, {"quality":%d,"cdn":%d}).
//  3. ISS/parent (a node IP from getnode) — the actual stream. Opcodes 0x9c4x
//     (see below). ISS_STREAM_DATA payload is the 77-byte SOOP chunk container
//     (H.264 Annex-B + ADTS AAC) already handled by the soop deframer.
//
// A pure-leaf join (ISS_JOIN → receive ISS_STREAM_DATA, serve no peers) gives the
// full stream with minimal upload contribution.

// ISS protocol opcodes (CNetControlObjectMb::ISSSvcProc dispatch, FUN_1001ed50).
const (
	ISS_JOIN_STC2STS_V3        = 0x9c4b // client→server join (payload 0xa18=2584B)
	ISS_JOIN_STS2STC           = 0x9c41 // server→client join reply
	ISS_JOIN_STS2STC_V2        = 0x9c46
	ISS_STREAM_DATA_STS2STC    = 0x9c44 // media (payload at header+16)
	ISS_STREAM_DATA_STS2STC_V2 = 0x9c47 // media V2 (payload at header+20) — the 77-byte chunks
	ISS_RECV_STREAM_STATUS     = 0x9c49
	ISS_STREAM_CONTROL         = 0x9c4a
	ISS_PUSH_STOP_NOTIFY       = 0x9c4c
	ISS_REQ_43                 = 0x9c43 // client send (payload 0x100)
	ISS_REQ_42                 = 0x9c42 // client send (payload 0x504)
)

// P2P (peer mesh) opcodes — not needed for the ISS leaf path, kept for reference.
// REQ_CACHE_DATA_VER2 → P2P_REP_CACHE2_DATA (cached chunks from a parent peer),
// P2P_ACK_BROADCAST_STREAM_RIGHT, P2P_HEALTH_CHECK, P2P_REP_BUDS_DATA (peer list).

// issHeader is the inner ISS packet header as parsed by ISSSvcProc: the dispatch
// reads *param_2 = opcode, param_2[1] = flag/result (negative ⇒ error),
// param_2[2] = length. Payload follows. STREAM_DATA_V2 starts payload at word 5
// (offset 20); most others at word 4 (offset 16).
type issHeader struct {
	Opcode uint32
	Flag   int32
	Length uint32
	W3     uint32
	W4     uint32
}

const issHeaderWords = 5 // 20 bytes; V2 stream-data payload begins here

// parseISSHeader reads the 5-word inner header from a decrypted/deframed packet.
func parseISSHeader(b []byte) (issHeader, bool) {
	if len(b) < 20 {
		return issHeader{}, false
	}
	return issHeader{
		Opcode: binary.LittleEndian.Uint32(b[0:]),
		Flag:   int32(binary.LittleEndian.Uint32(b[4:])),
		Length: binary.LittleEndian.Uint32(b[8:]),
		W3:     binary.LittleEndian.Uint32(b[12:]),
		W4:     binary.LittleEndian.Uint32(b[16:]),
	}, true
}

// buildBroadKey formats the stream key the way FUN_1001e230 does:
//
//	sprintf(dst, "%d-%s-%s-%s", bno, resourceDomain, quality, suffix)
//
// e.g. "295847647-<rmd>-original-<x>". quality "original" for 1080p.
func buildBroadKey(bno, resourceDomain, quality, suffix string) string {
	return bno + "-" + resourceDomain + "-" + quality + "-" + suffix
}

// aesDecryptEncKey decrypts a gateway "recommend/enckey" blob with the static
// key (CNetControlObject::_RecommendEncKeyData). The result is the session key
// material used for the rest of the protocol. Blob must be block-aligned.
func aesDecryptEncKey(blob []byte) ([]byte, error) {
	return decryptCBC(aesKey, aesIV, blob)
}

// keep the alt key / encrypt path referenced until the gateway handshake wires
// them in (FUN_10038080 selects aesAltKey for keylen selectors 7–9).
var _ = aesAltKey
var _ = encryptCBC
