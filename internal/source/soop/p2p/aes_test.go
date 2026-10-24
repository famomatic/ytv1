package p2p

import (
	"bytes"
	"testing"
)

func TestCBCVector(t *testing.T) {
	// NIST SP 800-38A F.2.1 (AES-128-CBC), first block.
	key := mustHex("2b7e151628aed2a6abf7158809cf4f3c")
	iv := mustHex("000102030405060708090a0b0c0d0e0f")
	plain := mustHex("6bc1bee22e409f96e93d7e117393172a")
	ciphertext := mustHex("7649abac8119b246cee98e9b12e9197d")
	got, err := encryptCBC(key, iv, plain)
	if err != nil || !bytes.Equal(got, ciphertext) {
		t.Fatalf("encrypt: %x, %v", got, err)
	}
	got, err = decryptCBC(key, iv, ciphertext)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypt: %x, %v", got, err)
	}
}

func TestCBCRejectsInvalidSizes(t *testing.T) {
	for name, fn := range map[string]func([]byte, []byte, []byte) ([]byte, error){
		"encrypt": encryptCBC, "decrypt": decryptCBC,
	} {
		t.Run(name, func(t *testing.T) {
			for _, sizes := range [][3]int{{15, 16, 16}, {16, 15, 16}, {16, 17, 16}, {16, 16, 15}, {16, 16, 17}} {
				if _, err := fn(make([]byte, sizes[0]), make([]byte, sizes[1]), make([]byte, sizes[2])); err == nil {
					t.Errorf("accepted key/iv/input lengths %v", sizes)
				}
			}
		})
	}
}
