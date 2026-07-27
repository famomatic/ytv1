// Package p2p reimplements SOOP's native P2P grid client (the NetControl.dll
// "SOOP Live P2P MultiStream Engine") so ytv1 can pull the ORIGINAL/1080p live
// stream directly from a KR IP with no VPN, no login, and no local SOOP agent.
//
// It joins as an ISS leaf (ISS_JOIN → ISS_STREAM_DATA), i.e. it pulls the stream
// from the source/relay and serves no peers, so upload contribution is minimal.
//
// Wire stack (per the decompiled NetControl.dll):
//
//	TCP  ─►  outer length frame  ─►  AES-CBC body  ─►  [opcode u32][flag u32][len u32]…[payload]
//
// The AES key/IV are static constants baked into NetControl.dll (.rdata), so the
// transport is fully reproducible. This file carries those constants and the
// block cipher; framing/opcodes/handshake live in protocol.go / client.go.
package p2p

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
)

// Static AES-128 key + IV, extracted from NetControl.dll .rdata:
//
//	DAT_10058250 (VA 0x10058250) → key   (FUN_10038010 copies 16 bytes as key)
//	DAT_10058260 (VA 0x10058260) → IV    (16 bytes)
//	DAT_10058280 (VA 0x10058280) → alt key for keylen selectors 7–9 (FUN_10038080)
//
// FUN_100380d0 runs the CBC block loop; the .rdata InvMixColumns tables
// (0x0e/0x09/0x0d/0x0b) confirm table-based AES. Case 6 in FUN_10038080 selects
// the DAT_10058260 material, cases 7–9 select DAT_10058280.
var (
	aesKey    = mustHex("45cb101d263d47515b64757b858f999f")
	aesIV     = mustHex("b6c3cadde7f1fb040e182228323c464c")
	aesAltKey = mustHex("5733c5a716f5dc133cca6291f2cb4668")
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("p2p: bad static key: " + err.Error())
	}
	return b
}

// decryptCBC AES-128-CBC-decrypts src in place-safe (returns a fresh slice).
// len(src) must be a multiple of the AES block size (16).
func decryptCBC(key, iv, src []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(src))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, src)
	return out, nil
}

// encryptCBC AES-128-CBC-encrypts src (a fresh slice). src must be block-aligned;
// callers pad to 16 bytes first (see padPKCS-free padding in protocol.go).
func encryptCBC(key, iv, src []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(src))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, src)
	return out, nil
}
