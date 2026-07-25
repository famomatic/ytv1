package p2p

// op2a session-auth blob (the 0x2a ISS packet body). SOOP's P2P engine sends this
// right after the op2/op3/op3e/op14 init to authenticate the viewer session to the
// relay. The construction was recovered from NetControl.dll (FUN_10037760) and is
// validated byte-for-byte against a captured blob in authblob_test.go.
//
// Wire body of op2a = [uint32 type=8][uint32 len][blob], where blob is:
//
//	blob[0:16]   = authHash(pbVar2)                         (also the pass-2 CTR key)
//	blob[16:]    = AES-128-CTR(key=blob[0:16], iv=authIV, pbVar2_padded16)
//	pbVar2       = [scramble(salt2) 18][c1 80][zero-pad to 16]
//	c1           = AES-128-CTR(key=authKey, iv=authIV, puVar1_padded16)
//	puVar1       = [header 16][scramble(salt1) 12][input]
//
// The cipher is AES-128-CTR in both passes (FUN_10036300 is CTR: xor-keystream +
// big-endian counter increment). "scramble" adds a small per-index constant table
// (FUN_10037510); the header packs timestamp / counter / checksum / input-length in
// a fixed interleaved order (the puVar1[0..15] assignments).

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
)

// Static constants extracted from NetControl.dll .rdata (mode-8 material).
var (
	authKey   = mustHexP2P("e2081129c4d0afbe55379fcde1755413") // DAT_100581f8
	authIV    = mustHexP2P("22655d8796eeca33c7a8221dffcb8271") // DAT_10058208
	authSalt1 = []byte("tkavudehd625")                         // DAT_10058218
	authSalt2 = []byte("tpqmsqpscjqoffl1-2")                   // DAT_10058224
	// per-index add table (DAT_10058238, i%6), applied as bytes: +1,-1,-3,-2,+1,+2
	authSaltTbl = []byte{0x01, 0xff, 0xfd, 0xfe, 0x01, 0x02}
)

func mustHexP2P(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("p2p: bad const: " + err.Error())
	}
	return b
}

// authCTR AES-128-CTR-transforms data (symmetric; encrypt == decrypt).
func authCTR(key, iv, data []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic("p2p: aes: " + err.Error())
	}
	out := make([]byte, len(data))
	cipher.NewCTR(block, iv).XORKeyStream(out, data)
	return out
}

func authScramble(dst, src []byte) {
	for i := range src {
		dst[i] = src[i] + authSaltTbl[i%6]
	}
}

func authUnscramble(src []byte) []byte {
	out := make([]byte, len(src))
	for i, c := range src {
		out[i] = c - authSaltTbl[i%6]
	}
	return out
}

// authChecksum is FUN_10037640 mode 8: a keyless rolling checksum over the input,
// seeded with (8<<24)|len, mixing each byte with an alternating OR/XOR.
func authChecksum(in []byte) uint32 {
	h := uint32(8)<<24 | uint32(len(in))
	for i, b := range in {
		sh := uint(((i % 5) << 4) & 0x1f)
		v := uint32(b) << sh
		if i&1 == 0 {
			h |= v
		} else {
			h ^= v
		}
	}
	return h
}

// authHeader packs the 16-byte scrambled header exactly as puVar1[0..15].
func authHeader(input []byte, ts, counter uint32) []byte {
	ln := uint32(len(input))
	k := authChecksum(input)
	var t, c, ck, l [4]byte
	binary.LittleEndian.PutUint32(t[:], ts)
	binary.LittleEndian.PutUint32(c[:], counter)
	binary.LittleEndian.PutUint32(ck[:], k)
	binary.LittleEndian.PutUint32(l[:], ln)
	h := make([]byte, 16)
	h[0], h[1], h[2], h[3] = t[0], ck[3], c[2], l[1]
	h[4], h[5], h[6], h[7] = t[2], ck[1], c[0], l[0]
	h[8], h[9], h[10], h[11] = t[3], ck[2], c[1], l[3]
	h[12], h[13], h[14], h[15] = t[1], ck[0], c[3], l[2]
	return h
}

func pad16(b []byte) []byte {
	if n := len(b) % 16; n != 0 {
		b = append(b, make([]byte, 16-n)...)
	}
	return b
}

// BuildAuthBlob produces the op2a session-auth blob for the given session input
// (the [2][8][0]+GUID payload) and freshly-chosen timestamp/counter. The result is
// the 128-ish byte blob that goes into the op2a packet body after the [8][len]
// prefix. ts is a unix-seconds-ish value; counter is an incrementing nonce.
func BuildAuthBlob(input []byte, ts, counter uint32) []byte {
	hdr := authHeader(input, ts, counter)
	s1 := make([]byte, len(authSalt1))
	authScramble(s1, authSalt1)

	puVar1 := make([]byte, 0, 16+len(s1)+len(input))
	puVar1 = append(puVar1, hdr...)
	puVar1 = append(puVar1, s1...)
	puVar1 = append(puVar1, input...)
	c1 := authCTR(authKey, authIV, pad16(puVar1))

	s2 := make([]byte, len(authSalt2))
	authScramble(s2, authSalt2)
	pbVar2 := pad16(append(append([]byte{}, s2...), c1...))

	mac := authHash(pbVar2)
	c2 := authCTR(mac, authIV, pbVar2)
	return append(append([]byte{}, mac...), c2...)
}

// SessionInput assembles the op2a plaintext input: [type=2][8][0] + 32-char GUID
// (uppercase hex, no dashes) + NUL. This is what the engine feeds to FUN_10037760.
func SessionInput(guid string) []byte {
	in := make([]byte, 12)
	binary.LittleEndian.PutUint32(in[0:], 2)
	binary.LittleEndian.PutUint32(in[4:], 8)
	// in[8:12] stays zero
	in = append(in, []byte(guid)...)
	in = append(in, 0)
	return in
}

// OpenAuthBlob reverses BuildAuthBlob: it returns the recovered input plaintext
// (header stripped) so the wire format can be inspected/validated. Mainly for tests
// and diagnostics.
func OpenAuthBlob(blob []byte) (header, input []byte, ok bool) {
	if len(blob) < 16+80 {
		return nil, nil, false
	}
	l2 := authCTR(blob[0:16], authIV, blob[16:])
	if len(l2) < 98 {
		return nil, nil, false
	}
	if s2 := authUnscramble(l2[0:18]); string(s2) != string(authSalt2) {
		return nil, nil, false
	}
	l1 := authCTR(authKey, authIV, l2[18:98])
	if s1 := authUnscramble(l1[16:28]); string(s1) != string(authSalt1) {
		return nil, nil, false
	}
	return l1[0:16], l1[28:], true
}
