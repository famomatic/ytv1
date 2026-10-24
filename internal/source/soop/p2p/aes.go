// Package p2p contains the recovered SOOP native grid protocol components.
// Gateway and relay framing are plaintext; the session-auth envelope uses
// AES-CTR (authblob.go). The CBC helpers here are retained for the historical
// item-service investigation and are not a whole-packet transport cipher.
package p2p

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
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
	if len(iv) != block.BlockSize() || len(src)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("p2p: CBC requires a %d-byte IV and block-aligned input", block.BlockSize())
	}
	out := make([]byte, len(src))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, src)
	return out, nil
}

// encryptCBC AES-128-CBC-encrypts src (a fresh slice). src must be block-aligned;
// callers supply any protocol-specific padding.
func encryptCBC(key, iv, src []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != block.BlockSize() || len(src)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("p2p: CBC requires a %d-byte IV and block-aligned input", block.BlockSize())
	}
	out := make([]byte, len(src))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, src)
	return out, nil
}
