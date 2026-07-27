package p2p

import (
	"bytes"
	"encoding/hex"
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
	if !bytes.HasPrefix(input, want) && !bytes.Equal(input[:len(want)], want) {
		t.Fatalf("recovered input mismatch:\n got %x\nwant %x", input, want)
	}
	if !bytes.Contains(input, []byte(capturedGUID)) {
		t.Fatalf("recovered input missing GUID: %x", input)
	}
}
