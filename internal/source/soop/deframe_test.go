package soop

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// makeChunk builds one 77-byte-header chunk wrapping payload.
func makeChunk(payload []byte) []byte {
	h := make([]byte, chunkHeaderLen)
	for i := range 8 {
		h[i] = 0xFF
	}
	binary.LittleEndian.PutUint32(h[8:], 0x00450001)
	binary.LittleEndian.PutUint32(h[16:], uint32(len(payload)))
	return append(h, payload...)
}

func TestDeframerClassifiesAndSplits(t *testing.T) {
	video := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x2A} // Annex-B SPS
	audio := []byte{0xFF, 0xF1, 0x50, 0x80, 0x00, 0x1F, 0xFC}       // ADTS
	stream := append(makeChunk(video), makeChunk(audio)...)

	var gotV, gotA []byte
	var d deframer
	d.push(stream, func(kind chunkKind, _, p []byte) {
		switch kind {
		case chunkVideo:
			gotV = append(gotV, p...)
		case chunkAudio:
			gotA = append(gotA, p...)
		}
	})
	if !bytes.Equal(gotV, video) {
		t.Fatalf("video mismatch: got %x want %x", gotV, video)
	}
	if !bytes.Equal(gotA, audio) {
		t.Fatalf("audio mismatch: got %x want %x", gotA, audio)
	}
}

func TestDeframerReassemblesSplitChunks(t *testing.T) {
	video := bytes.Repeat([]byte{0x00, 0x00, 0x00, 0x01, 0xAB}, 40) // 200-byte payload
	full := makeChunk(video)

	var got []byte
	var d deframer
	// Push 3-byte slices to stress carry handling across the header and payload.
	for i := 0; i < len(full); i += 3 {
		end := min(i+3, len(full))
		d.push(full[i:end], func(kind chunkKind, _, p []byte) {
			if kind == chunkVideo {
				got = append(got, p...)
			}
		})
	}
	if !bytes.Equal(got, video) {
		t.Fatalf("reassembled payload mismatch: got %d bytes want %d", len(got), len(video))
	}
}

func TestClassifyPayload(t *testing.T) {
	if classifyPayload([]byte{0, 0, 0, 1, 0x65}) != chunkVideo {
		t.Fatal("annex-b not classified as video")
	}
	if classifyPayload([]byte{0xFF, 0xF9, 0x00}) != chunkAudio {
		t.Fatal("adts not classified as audio")
	}
	if classifyPayload([]byte{0x01, 0x02}) != chunkOther {
		t.Fatal("unknown not classified as other")
	}
}
