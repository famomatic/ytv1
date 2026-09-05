package timeshift

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPTSClockRecovery(t *testing.T) {
	for _, stamps := range [][]int64{{(1 << 33) - 90000, 0, 90000}, {900000, 0, 90000}} {
		var out []Segment
		s := NewTSSegmenter(time.Second, func(v Segment) { out = append(out, v) })
		s.Write(patPacket())
		s.Write(pmtPacket())
		for i, pts := range stamps {
			s.Write(videoPES(pts, i, true))
		}
		s.Close()
		if len(out) != 3 {
			t.Fatalf("clock %v: got %d segments", stamps, len(out))
		}
		for _, seg := range out {
			if seg.Duration <= 0 || seg.Duration > 2*time.Second {
				t.Fatal(seg.Duration)
			}
		}
	}
}

func TestHEVCKeyframeAcrossPackets(t *testing.T) {
	var out []Segment
	s := NewTSSegmenter(time.Second, func(v Segment) { out = append(out, v) })
	s.vidPID = 256
	s.codec = 0x24
	pes := append([]byte{0, 0, 1, 0xe0, 0, 0, 0x80, 0x80, 5}, encodePTS(0)...)
	pes = append(pes, bytes.Repeat([]byte{0xff}, 170)...)
	pes = append(pes, 0, 0, 1, byte(19<<1), 1)
	s.Write(tsPacket(256, true, 0, pes[:184]))
	s.Write(tsPacket(256, false, 1, pes[184:]))
	s.Close()
	if len(out) != 1 {
		t.Fatal("missed HEVC IDR spanning TS packets")
	}
}

func TestUnfinishedSegmentBound(t *testing.T) {
	s := NewTSSegmenter(time.Second, nil)
	s.MaxSegmentBytes = 1024
	s.Write(patPacket())
	s.Write(pmtPacket())
	s.Write(videoPES(0, 0, true))
	_, err := s.Write(bytes.Repeat(tsPacket(256, false, 1, []byte{1}), 20))
	if err == nil {
		t.Fatal("unbounded segment accepted")
	}
}

func TestDiskPlaybackAfterFinish(t *testing.T) {
	d := NewDVR(Config{Dir: t.TempDir(), TargetSegmentDuration: time.Second})
	defer d.Close()
	d.Write(buildTSStream(7))
	d.Finish()
	for _, path := range []string{"/index.m3u8", "/seg0.ts"} {
		w := httptest.NewRecorder()
		d.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if strings.HasSuffix(path, "m3u8") && !strings.Contains(w.Body.String(), "#EXT-X-ENDLIST") {
			t.Fatal("missing ENDLIST")
		}
	}
}
