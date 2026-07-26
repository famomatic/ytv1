package soop

import (
	"bytes"
	"testing"
)

func TestTSMuxerStructure(t *testing.T) {
	var buf bytes.Buffer
	m := newTSMuxer(&buf)
	videoAU := append([]byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x2A}, bytes.Repeat([]byte{0xAB}, 400)...)
	audioFrame := []byte{0xFF, 0xF1, 0x50, 0x80, 0x01, 0xFF, 0xFC, 0x00, 0x01, 0x02, 0x03, 0x04}
	// ADTS frame length is in bytes 3-5; set it to len(audioFrame)=12.
	audioFrame[3] = 0x50 | byte((12>>11)&0x03)
	audioFrame[4] = byte(12 >> 3)
	audioFrame[5] = byte((12&0x07)<<5) | 0x1F

	if err := m.writeVideo(videoAU); err != nil {
		t.Fatal(err)
	}
	if err := m.writeAudio(audioFrame); err != nil {
		t.Fatal(err)
	}

	out := buf.Bytes()
	if len(out) == 0 || len(out)%tsLen != 0 {
		t.Fatalf("output not a whole number of 188-byte TS packets: %d", len(out))
	}
	var sawPAT, sawPMT, sawVideo, sawAudio bool
	for off := 0; off < len(out); off += tsLen {
		pkt := out[off : off+tsLen]
		if pkt[0] != tsSync {
			t.Fatalf("packet at %d missing sync byte 0x47", off)
		}
		pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
		switch pid {
		case 0x0000:
			sawPAT = true
		case tsPMT:
			sawPMT = true
		case tsVideo:
			sawVideo = true
		case tsAudio:
			sawAudio = true
		}
	}
	if !sawPAT || !sawPMT || !sawVideo || !sawAudio {
		t.Fatalf("missing PID: PAT=%v PMT=%v video=%v audio=%v", sawPAT, sawPMT, sawVideo, sawAudio)
	}
}

func TestSplitADTS(t *testing.T) {
	mk := func(n int) []byte {
		f := make([]byte, n)
		f[0], f[1] = 0xFF, 0xF1
		f[3] = byte((n >> 11) & 0x03)
		f[4] = byte(n >> 3)
		f[5] = byte((n & 0x07) << 5)
		return f
	}
	stream := append(mk(20), mk(15)...)
	frames := splitADTS(stream)
	if len(frames) != 2 || len(frames[0]) != 20 || len(frames[1]) != 15 {
		t.Fatalf("splitADTS wrong: got %d frames %v", len(frames), func() []int {
			var l []int
			for _, f := range frames {
				l = append(l, len(f))
			}
			return l
		}())
	}
}
