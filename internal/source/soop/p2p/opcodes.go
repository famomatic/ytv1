package p2p

// Opcodes recovered from NetControl.dll. All three protocols frame packets via
// the socket object's vtable[+0x5c] send(opcode, payload, len); the media path
// is plaintext (custom AES is item-service-only, §14.14 of the analysis doc).

// P2P peer-mesh opcodes (CNetControlObjectMb, V2S_* client→parent).
const (
	P2P_ACK_BROADCAST_STREAM_RIGHT = 0xcb21
	P2P_REQ_BROADCAST_STREAM_VER2  = 0xcb2b
	P2P_REQ_CACHE_HEADER_VER2      = 0xcb2e
	P2P_REQ_CACHE_HEADER_VER2_ALT  = 0xcb2f
	// V2S_REQ_CACHE_DATA_VER2: client→parent frame request. 16-byte payload =
	// quality bitmask + requested frame number (_DataInitHls).
	P2P_REQ_CACHE_DATA_VER2 = 0xcb30
)

// Quality bitmask for the cache request (from the quality selector in
// _DataInitHls: iVar5 → mask).
const (
	QMaskSD       = 0x01 // 360p (sel 2)
	QMaskHD       = 0x02 // 540p (sel 1)
	QMaskHD4k     = 0x08 // 720p (sel 4)
	QMaskOriginal = 0x10 // 1080p (sel 10)
	QMask1440p    = 0x20 // (sel 11)
)

// Gateway/control low opcodes seen on the socket send path (partial; exact
// field layouts come from the captured plaintext packets, §14.9).
const (
	opClientType  = 0x19 // cli_type + guid + addinfo body
	opBufferLeft  = 0x1b
	opKeepAlive1c = 0x1c
	opRecommend   = 0x2c
	opUtc         = 0x3e
	opCenterReq   = 0x63 // center getnode request (100-byte payload)
)

// selectorToQMask maps the internal quality selector to the cache-request mask.
func selectorToQMask(sel int) uint32 {
	switch sel {
	case 1:
		return QMaskHD
	case 2:
		return QMaskSD
	case 4:
		return QMaskHD4k
	case 10:
		return QMaskOriginal
	case 11:
		return QMask1440p
	default:
		return 0
	}
}

var _ = selectorToQMask
