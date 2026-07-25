package p2p

// authHash is SOOP's custom keyless hash used to MAC/derive the op2a session-auth
// blob (NetControl.dll FUN_1003a1a0 -> FUN_10039050 transform + FUN_1003a0d0
// finalize). It has MD5's init constants and MD5-style bit-length finalize, but an
// 8-round (128-step) transform with SHA-family round constants
// (5a827999/6ed9eba1/8f1bbcdc/50a28be6/5c4dd124/6d703ef3 + two constant-free
// rounds), permuted boolean functions and custom rotation amounts. It is a verbatim
// translation of the decompiled transform, validated byte-for-byte against a
// captured op2a blob (see authblob_test.go). NOTE: the finalize deliberately
// re-consumes the first (len%64) input bytes (not the tail) — that is what the
// binary does; do not "fix" it.
//
// This is not a general-purpose hash; it exists only to reproduce SOOP's wire
// format. Do not use it for anything security-sensitive.

func authHashLE32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func authHashPut32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

// authHash returns the 16-byte custom digest of data.
func authHash(data []byte) []byte {
	h := []uint32{0x67452301, 0xefcdab89, 0x98badcfe, 0x10325476}
	n := len(data)
	full := n &^ 0x3f
	m := make([]uint32, 16)
	for off := 0; off < full; off += 64 {
		for i := 0; i < 16; i++ {
			m[i] = authHashLE32(data[off+i*4:])
		}
		authBlock(h, m)
	}
	tail := data[:n&0x3f] // first len%64 bytes, per the binary
	for i := range m {
		m[i] = 0
	}
	for i := 0; i < len(tail); i++ {
		m[i>>2] ^= uint32(tail[i]) << uint((i&3)*8)
	}
	m[(n>>2)&0xf] ^= 1 << uint((n&3)*8+7)
	if len(tail) > 0x37 {
		authBlock(h, m)
		for i := range m {
			m[i] = 0
		}
	}
	m[14] = uint32(n) * 8
	m[15] = uint32(n) >> 0x1d
	authBlock(h, m)
	out := make([]byte, 0, 16)
	for _, x := range h {
		out = append(out, authHashPut32(x)...)
	}
	return out
}

// authBlock is the 128-step block transform (verbatim from FUN_10039050).
func authBlock(h []uint32, m []uint32) {
	var iVar1, iVar10, iVar11, iVar12, iVar13, iVar2, iVar3, iVar4, iVar5, iVar6, iVar7, iVar8, iVar9, uVar14, uVar15, uVar16, uVar17, uVar18, uVar19, uVar20, uVar21, uVar22, uVar23, uVar24 uint32
	uVar16 = h[2]
	uVar17 = h[1]
	uVar14 = (h[3] ^ uVar16 ^ uVar17) + m[0] + h[0]
	uVar14 = uVar14>>0x15 | uVar14*0x800
	uVar15 = h[3] + (uVar16 ^ uVar17 ^ uVar14) + m[1]
	uVar19 = uVar15>>0x12 | uVar15*0x4000
	uVar16 = uVar16 + (uVar19 ^ uVar17 ^ uVar14) + m[2]
	uVar15 = uVar16>>0x11 | uVar16*0x8000
	uVar17 = uVar17 + (uVar19 ^ uVar15 ^ uVar14) + m[3]
	uVar16 = uVar17>>0x14 | uVar17*0x1000
	uVar14 = uVar14 + (uVar19 ^ uVar15 ^ uVar16) + m[4]
	uVar17 = uVar14>>0x1b | uVar14*0x20
	iVar1 = m[6]
	uVar19 = uVar19 + (uVar15 ^ uVar16 ^ uVar17) + m[5]
	uVar14 = uVar19>>0x18 | uVar19*0x100
	uVar15 = uVar15 + (uVar14 ^ uVar16 ^ uVar17) + iVar1
	uVar15 = uVar15>>0x19 | uVar15*0x80
	iVar2 = m[7]
	uVar16 = uVar16 + (uVar14 ^ uVar15 ^ uVar17) + iVar2
	uVar16 = uVar16>>0x17 | uVar16*0x200
	uVar17 = uVar17 + (uVar14 ^ uVar15 ^ uVar16) + m[8]
	uVar17 = uVar17>>0x15 | uVar17*0x800
	iVar3 = m[9]
	uVar14 = uVar14 + (uVar15 ^ uVar16 ^ uVar17) + iVar3
	uVar14 = uVar14>>0x13 | uVar14*0x2000
	uVar15 = uVar15 + (uVar14 ^ uVar16 ^ uVar17) + m[10]
	uVar15 = uVar15>>0x12 | uVar15*0x4000
	iVar4 = m[0xc]
	uVar16 = uVar16 + (uVar14 ^ uVar15 ^ uVar17) + m[0xb]
	uVar16 = uVar16>>0x11 | uVar16*0x8000
	uVar17 = uVar17 + (uVar14 ^ uVar15 ^ uVar16) + iVar4
	iVar5 = m[0xd]
	uVar20 = uVar17>>0x1a | uVar17*0x40
	uVar14 = uVar14 + (uVar15 ^ uVar16 ^ uVar20) + iVar5
	uVar14 = uVar14>>0x19 | uVar14*0x80
	iVar6 = m[0xe]
	uVar15 = uVar15 + (uVar14 ^ uVar16 ^ uVar20) + iVar6
	iVar7 = m[0xf]
	uVar19 = uVar15>>0x17 | uVar15*0x200
	uVar16 = uVar16 + (uVar14 ^ uVar19 ^ uVar20) + iVar7
	uVar17 = uVar16>>0x18 | uVar16*0x100
	uVar16 = uVar20 + 0x5a827999 + (^uVar17&uVar14 | uVar19&uVar17) + iVar2
	uVar15 = uVar16>>0x19 | uVar16*0x80
	uVar16 = uVar14 + 0x5a827999 + (^uVar15&uVar19 | uVar17&uVar15) + m[4]
	uVar14 = uVar16>>0x1a | uVar16*0x40
	uVar16 = uVar19 + 0x5a827999 + (^uVar14&uVar17 | uVar14&uVar15) + iVar5
	uVar20 = uVar16>>0x18 | uVar16*0x100
	iVar8 = m[1]
	uVar16 = uVar17 + 0x5a827999 + (^uVar20&uVar15 | uVar14&uVar20) + iVar8
	uVar19 = uVar16>>0x13 | uVar16*0x2000
	iVar9 = m[10]
	uVar16 = uVar15 + 0x5a827999 + (^uVar19&uVar14 | uVar20&uVar19) + iVar9
	uVar17 = uVar16>>0x15 | uVar16*0x800
	uVar16 = uVar14 + 0x5a827999 + (^uVar17&uVar20 | uVar19&uVar17) + iVar1
	uVar14 = uVar16>>0x17 | uVar16*0x200
	uVar16 = uVar20 + 0x5a827999 + (^uVar14&uVar19 | uVar14&uVar17) + iVar7
	uVar15 = uVar16>>0x19 | uVar16*0x80
	iVar10 = m[3]
	uVar16 = uVar19 + 0x5a827999 + (^uVar15&uVar17 | uVar14&uVar15) + iVar10
	uVar20 = uVar16>>0x11 | uVar16*0x8000
	uVar16 = uVar17 + 0x5a827999 + (^uVar20&uVar14 | uVar15&uVar20) + iVar4
	uVar19 = uVar16>>0x19 | uVar16*0x80
	iVar11 = m[0]
	uVar16 = uVar14 + 0x5a827999 + (^uVar19&uVar15 | uVar20&uVar19) + iVar11
	uVar17 = uVar16>>0x14 | uVar16*0x1000
	uVar16 = uVar15 + 0x5a827999 + (^uVar17&uVar20 | uVar17&uVar19) + iVar3
	uVar14 = uVar16>>0x11 | uVar16*0x8000
	iVar12 = m[5]
	uVar16 = uVar20 + 0x5a827999 + (^uVar14&uVar19 | uVar17&uVar14) + iVar12
	uVar15 = uVar16>>0x17 | uVar16*0x200
	iVar13 = m[2]
	uVar16 = uVar19 + 0x5a827999 + (^uVar15&uVar17 | uVar14&uVar15) + iVar13
	uVar20 = uVar16>>0x15 | uVar16*0x800
	uVar16 = uVar17 + 0x5a827999 + (^uVar20&uVar14 | uVar15&uVar20) + iVar6
	uVar19 = uVar16>>0x19 | uVar16*0x80
	uVar16 = uVar14 + 0x5a827999 + (^uVar19&uVar15 | uVar19&uVar20) + m[0xb]
	uVar17 = uVar16>>0x13 | uVar16*0x2000
	uVar16 = uVar15 + 0x5a827999 + (^uVar17&uVar20 | uVar19&uVar17) + m[8]
	uVar21 = uVar16>>0x14 | uVar16*0x1000
	uVar16 = uVar20 + 0x6ed9eba1 + ((^uVar17 | uVar21) ^ uVar19) + iVar10
	uVar15 = uVar16>>0x15 | uVar16*0x800
	uVar16 = uVar19 + 0x6ed9eba1 + ((^uVar21 | uVar15) ^ uVar17) + iVar9
	uVar14 = uVar16>>0x13 | uVar16*0x2000
	uVar16 = uVar17 + 0x6ed9eba1 + ((^uVar15 | uVar14) ^ uVar21) + iVar6
	uVar17 = uVar16>>0x1a | uVar16*0x40
	uVar16 = uVar21 + 0x6ed9eba1 + ((^uVar14 | uVar17) ^ uVar15) + m[4]
	uVar19 = uVar16>>0x19 | uVar16*0x80
	uVar16 = uVar15 + 0x6ed9eba1 + ((^uVar17 | uVar19) ^ uVar14) + iVar3
	uVar15 = uVar16>>0x12 | uVar16*0x4000
	uVar16 = uVar14 + 0x6ed9eba1 + ((^uVar19 | uVar15) ^ uVar17) + iVar7
	uVar14 = uVar16>>0x17 | uVar16*0x200
	uVar16 = uVar17 + 0x6ed9eba1 + ((^uVar15 | uVar14) ^ uVar19) + m[8]
	uVar17 = uVar16>>0x13 | uVar16*0x2000
	uVar16 = uVar19 + 0x6ed9eba1 + ((^uVar14 | uVar17) ^ uVar15) + iVar8
	uVar19 = uVar16>>0x11 | uVar16*0x8000
	uVar16 = uVar15 + 0x6ed9eba1 + ((^uVar17 | uVar19) ^ uVar14) + iVar13
	uVar20 = uVar16>>0x12 | uVar16*0x4000
	uVar16 = uVar14 + 0x6ed9eba1 + ((^uVar19 | uVar20) ^ uVar17) + iVar2
	uVar15 = uVar16>>0x18 | uVar16*0x100
	uVar16 = uVar17 + 0x6ed9eba1 + ((^uVar20 | uVar15) ^ uVar19) + iVar11
	uVar14 = uVar16>>0x13 | uVar16*0x2000
	uVar16 = uVar19 + 0x6ed9eba1 + ((^uVar15 | uVar14) ^ uVar20) + iVar1
	uVar19 = uVar16>>0x1a | uVar16*0x40
	uVar16 = uVar20 + 0x6ed9eba1 + ((^uVar14 | uVar19) ^ uVar15) + iVar5
	uVar20 = uVar16>>0x1b | uVar16*0x20
	uVar16 = uVar15 + 0x6ed9eba1 + ((^uVar19 | uVar20) ^ uVar14) + m[0xb]
	uVar17 = uVar16>>0x14 | uVar16*0x1000
	uVar16 = uVar14 + 0x6ed9eba1 + ((^uVar20 | uVar17) ^ uVar19) + iVar12
	uVar14 = uVar16>>0x19 | uVar16*0x80
	uVar16 = uVar19 + 0x6ed9eba1 + ((^uVar17 | uVar14) ^ uVar20) + m[0xc]
	uVar15 = uVar16>>0x1b | uVar16*0x20
	uVar16 = uVar20 + 0x8f1bbcdc + (^uVar17&uVar14 | uVar17&uVar15) + iVar8
	uVar20 = uVar16>>0x15 | uVar16*0x800
	uVar17 = (^uVar14&uVar15 | uVar14&uVar20) + iVar3 + 0x8f1bbcdc + uVar17
	uVar19 = uVar17>>0x14 | uVar17*0x1000
	uVar16 = uVar14 + 0x8f1bbcdc + (^uVar15&uVar20 | uVar19&uVar15) + m[0xb]
	uVar21 = uVar16>>0x12 | uVar16*0x4000
	uVar16 = uVar15 + 0x8f1bbcdc + (^uVar20&uVar19 | uVar21&uVar20) + iVar9
	uVar17 = uVar16>>0x11 | uVar16*0x8000
	uVar16 = uVar20 + 0x8f1bbcdc + (^uVar19&uVar21 | uVar19&uVar17) + iVar11
	uVar14 = uVar16>>0x12 | uVar16*0x4000
	uVar16 = uVar19 + 0x8f1bbcdc + (^uVar21&uVar17 | uVar21&uVar14) + m[8]
	uVar15 = uVar16>>0x11 | uVar16*0x8000
	uVar16 = uVar21 + 0x8f1bbcdc + (^uVar17&uVar14 | uVar15&uVar17) + m[0xc]
	uVar20 = uVar16>>0x17 | uVar16*0x200
	uVar16 = uVar17 + 0x8f1bbcdc + (^uVar14&uVar15 | uVar20&uVar14) + m[4]
	uVar19 = uVar16>>0x18 | uVar16*0x100
	uVar16 = uVar14 + 0x8f1bbcdc + (^uVar15&uVar20 | uVar15&uVar19) + iVar5
	uVar21 = uVar16>>0x17 | uVar16*0x200
	uVar16 = uVar15 + 0x8f1bbcdc + (^uVar20&uVar19 | uVar20&uVar21) + iVar10
	uVar17 = uVar16>>0x12 | uVar16*0x4000
	uVar16 = uVar20 + 0x8f1bbcdc + (^uVar19&uVar21 | uVar17&uVar19) + iVar2
	uVar14 = uVar16>>0x1b | uVar16*0x20
	uVar16 = uVar19 + 0x8f1bbcdc + (^uVar21&uVar17 | uVar14&uVar21) + iVar7
	uVar15 = uVar16>>0x1a | uVar16*0x40
	uVar16 = uVar21 + 0x8f1bbcdc + (^uVar17&uVar14 | uVar17&uVar15) + m[0xe]
	uVar18 = uVar16>>0x18 | uVar16*0x100
	uVar16 = uVar17 + 0x8f1bbcdc + (^uVar14&uVar15 | uVar14&uVar18) + m[5]
	uVar22 = uVar16>>0x1a | uVar16*0x40
	uVar16 = uVar14 + 0x8f1bbcdc + (^uVar15&uVar18 | uVar22&uVar15) + m[6]
	uVar20 = uVar16>>0x1b | uVar16*0x20
	uVar16 = uVar15 + 0x8f1bbcdc + (^uVar18&uVar22 | uVar20&uVar18) + m[2]
	uVar14 = h[3]
	uVar15 = h[2]
	uVar19 = h[1]
	uVar17 = (^uVar14&uVar15 | uVar14&uVar19) + m[5] + 0x50a28be6 + h[0]
	uVar21 = uVar17>>0x18 | uVar17*0x100
	uVar17 = uVar14 + 0x50a28be6 + (^uVar15&uVar19 | uVar15&uVar21) + m[0xe]
	uVar14 = uVar17>>0x17 | uVar17*0x200
	uVar17 = uVar15 + 0x50a28be6 + (^uVar19&uVar21 | uVar14&uVar19) + iVar2
	uVar15 = uVar17>>0x17 | uVar17*0x200
	uVar17 = uVar19 + 0x50a28be6 + (^uVar21&uVar14 | uVar15&uVar21) + iVar11
	uVar19 = uVar17>>0x15 | uVar17*0x800
	uVar17 = uVar21 + 0x50a28be6 + (^uVar14&uVar15 | uVar14&uVar19) + iVar3
	uVar21 = uVar17>>0x13 | uVar17*0x2000
	uVar17 = uVar14 + 0x50a28be6 + (^uVar15&uVar19 | uVar15&uVar21) + m[2]
	uVar14 = uVar17>>0x11 | uVar17*0x8000
	uVar17 = uVar15 + 0x50a28be6 + (^uVar19&uVar21 | uVar14&uVar19) + m[0xb]
	uVar15 = uVar17>>0x11 | uVar17*0x8000
	uVar17 = uVar19 + 0x50a28be6 + (^uVar21&uVar14 | uVar15&uVar21) + m[4]
	uVar19 = uVar17>>0x1b | uVar17*0x20
	uVar17 = uVar21 + 0x50a28be6 + (^uVar14&uVar15 | uVar14&uVar19) + iVar5
	uVar21 = uVar17>>0x19 | uVar17*0x80
	uVar17 = uVar14 + 0x50a28be6 + (^uVar15&uVar19 | uVar15&uVar21) + m[6]
	uVar23 = uVar17>>0x19 | uVar17*0x80
	uVar17 = uVar15 + 0x50a28be6 + (^uVar19&uVar21 | uVar23&uVar19) + iVar7
	uVar14 = uVar17>>0x18 | uVar17*0x100
	uVar17 = uVar19 + 0x50a28be6 + (^uVar21&uVar23 | uVar14&uVar21) + m[8]
	uVar19 = uVar17>>0x15 | uVar17*0x800
	uVar17 = uVar21 + 0x50a28be6 + (^uVar23&uVar14 | uVar23&uVar19) + iVar8
	uVar15 = uVar17>>0x12 | uVar17*0x4000
	uVar17 = uVar23 + 0x50a28be6 + (^uVar14&uVar19 | uVar14&uVar15) + m[10]
	uVar21 = uVar17>>0x12 | uVar17*0x4000
	uVar17 = uVar14 + 0x50a28be6 + (^uVar19&uVar15 | uVar21&uVar19) + m[3]
	uVar23 = uVar17>>0x14 | uVar17*0x1000
	uVar17 = uVar19 + 0x50a28be6 + (^uVar15&uVar21 | uVar23&uVar15) + iVar4
	uVar14 = uVar17>>0x1a | uVar17*0x40
	uVar17 = uVar15 + 0x5c4dd124 + ((^uVar23 | uVar14) ^ uVar21) + iVar1
	uVar19 = uVar17>>0x17 | uVar17*0x200
	uVar17 = uVar21 + 0x5c4dd124 + ((^uVar14 | uVar19) ^ uVar23) + m[0xb]
	uVar15 = uVar17>>0x13 | uVar17*0x2000
	uVar17 = uVar23 + 0x5c4dd124 + ((^uVar19 | uVar15) ^ uVar14) + m[3]
	uVar21 = uVar17>>0x11 | uVar17*0x8000
	uVar17 = uVar14 + 0x5c4dd124 + ((^uVar15 | uVar21) ^ uVar19) + iVar2
	uVar23 = uVar17>>0x19 | uVar17*0x80
	uVar17 = uVar19 + 0x5c4dd124 + ((^uVar21 | uVar23) ^ uVar15) + iVar11
	uVar14 = uVar17>>0x14 | uVar17*0x1000
	uVar17 = uVar15 + 0x5c4dd124 + ((^uVar23 | uVar14) ^ uVar21) + iVar5
	uVar19 = uVar17>>0x18 | uVar17*0x100
	uVar17 = uVar21 + 0x5c4dd124 + ((^uVar14 | uVar19) ^ uVar23) + iVar12
	uVar15 = uVar17>>0x17 | uVar17*0x200
	uVar17 = uVar23 + 0x5c4dd124 + ((^uVar19 | uVar15) ^ uVar14) + m[10]
	uVar21 = uVar17>>0x15 | uVar17*0x800
	uVar17 = uVar14 + 0x5c4dd124 + ((^uVar15 | uVar21) ^ uVar19) + iVar6
	uVar23 = uVar17>>0x19 | uVar17*0x80
	uVar17 = uVar19 + 0x5c4dd124 + ((^uVar21 | uVar23) ^ uVar15) + m[0xf]
	uVar14 = uVar17>>0x19 | uVar17*0x80
	uVar17 = uVar15 + 0x5c4dd124 + ((^uVar23 | uVar14) ^ uVar21) + m[8]
	uVar19 = uVar17>>0x14 | uVar17*0x1000
	uVar17 = uVar21 + 0x5c4dd124 + ((^uVar14 | uVar19) ^ uVar23) + iVar4
	uVar15 = uVar17>>0x19 | uVar17*0x80
	uVar17 = uVar23 + 0x5c4dd124 + ((^uVar19 | uVar15) ^ uVar14) + m[4]
	uVar21 = uVar17>>0x1a | uVar17*0x40
	uVar17 = uVar14 + 0x5c4dd124 + ((^uVar15 | uVar21) ^ uVar19) + m[9]
	uVar23 = uVar17>>0x11 | uVar17*0x8000
	uVar17 = uVar19 + 0x5c4dd124 + ((^uVar21 | uVar23) ^ uVar15) + m[1]
	uVar19 = uVar17>>0x13 | uVar17*0x2000
	uVar17 = uVar15 + 0x5c4dd124 + ((^uVar23 | uVar19) ^ uVar21) + iVar13
	uVar14 = uVar17>>0x15 | uVar17*0x800
	uVar17 = uVar21 + 0x6d703ef3 + (^uVar14&uVar23 | uVar19&uVar14) + m[0xf]
	uVar15 = uVar17>>0x17 | uVar17*0x200
	uVar17 = uVar23 + 0x6d703ef3 + (^uVar15&uVar19 | uVar14&uVar15) + iVar12
	uVar21 = uVar17>>0x19 | uVar17*0x80
	uVar17 = uVar19 + 0x6d703ef3 + (^uVar21&uVar14 | uVar21&uVar15) + m[1]
	uVar23 = uVar17>>0x11 | uVar17*0x8000
	uVar17 = uVar14 + 0x6d703ef3 + (^uVar23&uVar15 | uVar21&uVar23) + iVar10
	uVar24 = uVar17>>0x15 | uVar17*0x800
	uVar17 = uVar15 + 0x6d703ef3 + (^uVar24&uVar21 | uVar23&uVar24) + iVar2
	uVar15 = uVar17>>0x18 | uVar17*0x100
	uVar17 = uVar21 + 0x6d703ef3 + (^uVar15&uVar23 | uVar24&uVar15) + iVar6
	uVar19 = uVar17>>0x1a | uVar17*0x40
	uVar17 = uVar23 + 0x6d703ef3 + (^uVar19&uVar24 | uVar19&uVar15) + iVar1
	uVar14 = uVar17>>0x1a | uVar17*0x40
	uVar17 = uVar24 + 0x6d703ef3 + (^uVar14&uVar15 | uVar19&uVar14) + m[9]
	uVar23 = uVar17>>0x12 | uVar17*0x4000
	uVar17 = uVar15 + 0x6d703ef3 + (^uVar23&uVar19 | uVar14&uVar23) + m[0xb]
	uVar24 = uVar17>>0x14 | uVar17*0x1000
	uVar17 = uVar19 + 0x6d703ef3 + (^uVar24&uVar14 | uVar23&uVar24) + m[8]
	uVar21 = uVar17>>0x13 | uVar17*0x2000
	uVar14 = (^uVar21&uVar23 | uVar21&uVar24) + iVar4 + 0x6d703ef3 + uVar14
	uVar15 = uVar14>>0x1b | uVar14*0x20
	uVar17 = uVar23 + 0x6d703ef3 + (^uVar15&uVar24 | uVar21&uVar15) + iVar13
	uVar14 = uVar17>>0x12 | uVar17*0x4000
	uVar17 = uVar24 + 0x6d703ef3 + (^uVar14&uVar21 | uVar15&uVar14) + iVar9
	uVar19 = uVar17>>0x13 | uVar17*0x2000
	uVar17 = uVar21 + 0x6d703ef3 + (^uVar19&uVar15 | uVar14&uVar19) + iVar11
	uVar21 = uVar17>>0x13 | uVar17*0x2000
	uVar17 = uVar15 + 0x6d703ef3 + (^uVar21&uVar14 | uVar21&uVar19) + m[4]
	uVar17 = uVar17>>0x19 | uVar17*0x80
	uVar14 = (^uVar17&uVar19 | uVar21&uVar17) + iVar5 + 0x6d703ef3 + uVar14
	uVar23 = uVar14>>0x1b | uVar14*0x20
	uVar19 = uVar19 + (uVar21 ^ uVar17 ^ uVar23) + m[8]
	uVar14 = uVar19>>0x11 | uVar19*0x8000
	uVar21 = uVar21 + (uVar17 ^ uVar23 ^ uVar14) + iVar1
	uVar19 = uVar21>>0x1b | uVar21*0x20
	uVar17 = uVar17 + (uVar19 ^ uVar23 ^ uVar14) + m[4]
	uVar15 = uVar17>>0x18 | uVar17*0x100
	uVar23 = uVar23 + (uVar19 ^ uVar15 ^ uVar14) + iVar8
	uVar17 = uVar23>>0x15 | uVar23*0x800
	uVar14 = uVar14 + (uVar19 ^ uVar15 ^ uVar17) + iVar10
	uVar14 = uVar14>>0x12 | uVar14*0x4000
	uVar19 = uVar19 + (uVar15 ^ uVar17 ^ uVar14) + m[0xb]
	uVar19 = uVar19>>0x12 | uVar19*0x4000
	uVar15 = uVar15 + (uVar19 ^ uVar17 ^ uVar14) + iVar7
	uVar21 = uVar15>>0x1a | uVar15*0x40
	uVar17 = uVar17 + (uVar19 ^ uVar21 ^ uVar14) + iVar11
	uVar15 = uVar17>>0x12 | uVar17*0x4000
	uVar14 = uVar14 + (uVar19 ^ uVar21 ^ uVar15) + iVar12
	uVar17 = uVar14>>0x1a | uVar14*0x40
	uVar19 = uVar19 + (uVar21 ^ uVar15 ^ uVar17) + iVar4
	uVar19 = uVar19>>0x17 | uVar19*0x200
	uVar21 = uVar21 + (uVar19 ^ uVar15 ^ uVar17) + iVar13
	uVar21 = uVar21>>0x14 | uVar21*0x1000
	uVar15 = uVar15 + (uVar19 ^ uVar21 ^ uVar17) + iVar5
	uVar14 = uVar15>>0x17 | uVar15*0x200
	uVar17 = uVar17 + (uVar19 ^ uVar21 ^ uVar14) + iVar3
	uVar15 = uVar17>>0x14 | uVar17*0x1000
	uVar19 = uVar19 + (uVar21 ^ uVar14 ^ uVar15) + iVar2
	uVar17 = uVar19>>0x1b | uVar19*0x20
	uVar21 = uVar21 + (uVar17 ^ uVar14 ^ uVar15) + iVar9
	uVar19 = uVar21>>0x11 | uVar21*0x8000
	uVar14 = uVar14 + (uVar17 ^ uVar19 ^ uVar15) + iVar6
	iVar1 = h[1]
	h[1] = h[2] + uVar15 + uVar22
	h[2] = (uVar14>>0x18 | uVar14*0x100) + h[3] + uVar18
	h[3] = (uVar16>>0x14 | uVar16*0x1000) + h[0] + uVar19
	h[0] = uVar17 + iVar1 + uVar20
}
