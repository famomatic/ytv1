package p2p

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"
)

// A real op2a blob captured from the SOOP P2P engine on the wire, plus the session
// values recovered from it. This is the ground-truth vector that pins the entire
// custom crypto (AES-128-CTR two-pass + the custom hash MAC + header packing).
const (
	capturedOp2aBlob = "cd4ee4b18cded3bd336b0a89e452cee3" +
		"8e3aa7c0e1189ead7d46c156d0c603d0" +
		"4a5c67f51dcaf0a602212dae091ff2fe" +
		"54b4f0b234b0534e96848cdad7d1dff4" +
		"f1b35d93e3f5fe30c272b1ec21b7cc74" +
		"09f22ab10ce01fa56d65b51f68e4afdb" +
		"c66bed3b95ee5e671e3cdb52ffa99149" +
		"709677d7c22cdeefeea083b1d0165286"
	capturedGUID    = "25631A3AB79CB882B26207735783A003"
	capturedTS      = 0x6a64fbd3
	capturedCounter = 0x00000677
)

func TestAuthHashVector(t *testing.T) {
	// authHash(pbVar2) must equal the MAC stored at blob[0:16].
	blob, _ := hex.DecodeString(capturedOp2aBlob)
	l2 := authCTR(blob[0:16], authIV, blob[16:])
	got := authHash(l2)
	if !bytes.Equal(got, blob[0:16]) {
		t.Fatalf("authHash mismatch:\n got %x\nwant %x", got, blob[0:16])
	}
}

func TestBuildAuthBlobReproducesCapture(t *testing.T) {
	want, _ := hex.DecodeString(capturedOp2aBlob)
	input := SessionInput(capturedGUID)
	got := BuildAuthBlob(input, capturedTS, capturedCounter)
	if !bytes.Equal(got, want) {
		t.Fatalf("BuildAuthBlob did not reproduce the captured blob:\n got %x\nwant %x", got, want)
	}
}

func TestOpenAuthBlobRoundTrip(t *testing.T) {
	blob, _ := hex.DecodeString(capturedOp2aBlob)
	_, input, ok := OpenAuthBlob(blob)
	if !ok {
		t.Fatal("OpenAuthBlob failed to validate the captured blob")
	}
	// input = [2][8][0] + GUID + NUL
	want := SessionInput(capturedGUID)
	if !bytes.Equal(input, want) {
		t.Fatalf("recovered input mismatch:\n got %x\nwant %x", input, want)
	}
	if !bytes.Contains(input, []byte(capturedGUID)) {
		t.Fatalf("recovered input missing GUID: %x", input)
	}
}

func TestAuthBlobVariableInput(t *testing.T) {
	for _, size := range []int{0, 1, 3, 4, 19, 20, 21, 35, 36, 37, 45, 52, 53, 63, 64, 65, 255, 256, 1024, 65536} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			want := make([]byte, size)
			for i := range want {
				want[i] = byte(i*17 + 3)
			}
			blob := BuildAuthBlob(want, 0x12345678, 0xabcdef01)
			header, got, ok := OpenAuthBlob(blob)
			if !ok || !bytes.Equal(got, want) || !bytes.Equal(header, authHeader(want, 0x12345678, 0xabcdef01)) {
				t.Fatalf("round trip: ok=%v input length=%d, want=%d", ok, len(got), size)
			}
		})
	}
}

func TestAuthBlobRejectsCorruption(t *testing.T) {
	blob, _ := hex.DecodeString(capturedOp2aBlob)
	for i := range blob {
		bad := bytes.Clone(blob)
		bad[i] ^= 1
		if header, input, ok := OpenAuthBlob(bad); ok {
			// The native hash reuses the first partial block, and its rolling
			// checksum has collisions. Some input changes are undetectable;
			// require a canonical envelope, not cryptographic authentication.
			ts := uint32(header[0]) | uint32(header[12])<<8 | uint32(header[4])<<16 | uint32(header[8])<<24
			counter := uint32(header[6]) | uint32(header[10])<<8 | uint32(header[2])<<16 | uint32(header[14])<<24
			if !bytes.Equal(BuildAuthBlob(input, ts, counter), bad) {
				t.Fatalf("accepted noncanonical changed byte %d", i)
			}
		}
		if _, _, ok := OpenAuthBlob(blob[:i]); ok {
			t.Fatalf("accepted truncated length %d", i)
		}
	}
	if _, _, ok := OpenAuthBlob(append(bytes.Clone(blob), make([]byte, 16)...)); ok {
		t.Fatal("accepted trailing block")
	}
}

func TestAuthBlobRejectsInvalidEnvelopeWithMatchingDigest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]byte, []byte)
	}{
		{"outer salt", func(outer, inner []byte) { outer[0] ^= 1 }},
		{"inner salt", func(outer, inner []byte) { inner[16] ^= 1 }},
		{"checksum", func(outer, inner []byte) { inner[13] ^= 1 }},
		{"huge length", func(outer, inner []byte) {
			inner[7], inner[3], inner[15], inner[11] = 255, 255, 255, 255
		}},
		{"inner padding", func(outer, inner []byte) { inner[79] = 1 }},
		{"outer padding", func(outer, inner []byte) { outer[len(outer)-1] = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blob, _ := hex.DecodeString(capturedOp2aBlob)
			outer := authCTR(blob[:16], authIV, blob[16:])
			inner := authCTR(authKey, authIV, outer[18:98])
			tc.mutate(outer, inner)
			copy(outer[18:98], authCTR(authKey, authIV, inner))
			digest := authHash(outer)
			bad := append(bytes.Clone(digest), authCTR(digest, authIV, outer)...)
			if _, _, ok := OpenAuthBlob(bad); ok {
				t.Fatal("accepted invalid envelope with a recomputed digest")
			}
		})
	}
}

func FuzzOpenAuthBlob(f *testing.F) {
	captured, _ := hex.DecodeString(capturedOp2aBlob)
	f.Add(captured)
	f.Add(BuildAuthBlob(nil, 0, 0))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, blob []byte) {
		header, input, ok := OpenAuthBlob(blob)
		if !ok {
			return
		}
		ts := uint32(header[0]) | uint32(header[12])<<8 | uint32(header[4])<<16 | uint32(header[8])<<24
		counter := uint32(header[6]) | uint32(header[10])<<8 | uint32(header[2])<<16 | uint32(header[14])<<24
		if rebuilt := BuildAuthBlob(input, ts, counter); !bytes.Equal(rebuilt, blob) {
			t.Fatal("accepted a noncanonical blob")
		}
	})
}
