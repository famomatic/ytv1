package timeshift

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestSegmenterRealData feeds a real captured MPEG-TS file (from the SOOP/puremux
// path) through the segmenter and checks every emitted segment is well-formed:
// whole 188-byte packets, starting with a PAT and PMT, and containing a video
// keyframe. Opt-in: set YTV1_TS_SAMPLE=<path.ts> (e.g. a scratch capture).
func TestSegmenterRealData(t *testing.T) {
	path := os.Getenv("YTV1_TS_SAMPLE")
	if path == "" {
		t.Skip("set YTV1_TS_SAMPLE=<path.ts> to run")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}

	var segs []Segment
	seg := NewTSSegmenter(3*time.Second, func(s Segment) { segs = append(segs, s) })
	// Feed in odd-sized chunks to exercise packet reassembly.
	for i := 0; i < len(data); i += 4096 + 7 {
		end := min(i+4096+7, len(data))
		_, _ = seg.Write(data[i:end])
	}
	seg.Close()

	if len(segs) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segs))
	}
	dumpDir := os.Getenv("YTV1_TS_DUMP")
	var totalDur time.Duration
	for _, s := range segs {
		totalDur += s.Duration
		if len(s.Data)%tsPacketLen != 0 {
			t.Fatalf("seg %d not packet-aligned: %d bytes", s.Seq, len(s.Data))
		}
		assertSegmentWellFormed(t, s)
		if dumpDir != "" {
			_ = os.WriteFile(fmt.Sprintf("%s/seg%d.ts", dumpDir, s.Seq), s.Data, 0o644)
		}
	}
	t.Logf("segments=%d total=%.1fs avg=%.2fs", len(segs), totalDur.Seconds(), totalDur.Seconds()/float64(len(segs)))
}

// assertSegmentWellFormed checks a segment starts with PAT then PMT and carries a
// video keyframe access unit.
func assertSegmentWellFormed(t *testing.T, s Segment) {
	t.Helper()
	var sawPAT, sawPMT, sawKF bool
	var pmtPID, vidPID = -1, -1
	for off := 0; off+tsPacketLen <= len(s.Data); off += tsPacketLen {
		pkt := s.Data[off : off+tsPacketLen]
		if pkt[0] != 0x47 {
			t.Fatalf("seg %d off %d: bad sync 0x%02x", s.Seq, off, pkt[0])
		}
		pid := (int(pkt[1]&0x1F) << 8) | int(pkt[2])
		pusi := pkt[1]&0x40 != 0
		switch {
		case pid == 0 && pusi:
			sawPAT = true
			pmtPID = parsePATFirstPMTPID(payloadOf(pkt, true))
		case pid == pmtPID && pusi:
			sawPMT = true
			vidPID = parsePMTVideoPID(payloadOf(pkt, true))
		case pid == vidPID && pusi:
			if isKeyframePES(payloadOf(pkt, false)) {
				sawKF = true
			}
		}
	}
	if !sawPAT || !sawPMT || !sawKF {
		t.Fatalf("seg %d not self-contained: PAT=%v PMT=%v keyframe=%v", s.Seq, sawPAT, sawPMT, sawKF)
	}
	// The first video keyframe should be at (or very near) the segment start:
	// PAT + PMT + keyframe within the first few packets.
	if s.Seq >= 0 && !firstVideoIsKeyframe(s.Data, vidPID) {
		t.Fatalf("seg %d: first video packet is not a keyframe", s.Seq)
	}
}

// firstVideoIsKeyframe reports whether the first video-PID PUSI packet in the
// segment starts a keyframe access unit.
func firstVideoIsKeyframe(data []byte, vidPID int) bool {
	for off := 0; off+tsPacketLen <= len(data); off += tsPacketLen {
		pkt := data[off : off+tsPacketLen]
		pid := (int(pkt[1]&0x1F) << 8) | int(pkt[2])
		if pid == vidPID && pkt[1]&0x40 != 0 {
			return isKeyframePES(payloadOf(pkt, false))
		}
	}
	return false
}
