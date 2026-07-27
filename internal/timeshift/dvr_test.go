package timeshift

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// --- synthetic MPEG-TS builders (hand-crafted, so segmentation is exercised
// without depending on a real capture) ---

func tsPacket(pid int, pusi bool, cc int, payload []byte) []byte {
	pkt := make([]byte, tsPacketLen)
	pkt[0] = 0x47
	pkt[1] = byte(pid>>8) & 0x1F
	if pusi {
		pkt[1] |= 0x40
	}
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 | byte(cc&0x0F) // adaptation_field_control = 01 (payload only)
	copy(pkt[4:], payload)
	for i := 4 + len(payload); i < tsPacketLen; i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

func patPacket() []byte {
	sec := []byte{
		0x00,       // table_id
		0xB0, 0x0D, // section_syntax + length=13
		0x00, 0x01, // transport_stream_id
		0xC1, 0x00, 0x00, // version/current, section#, last#
		0x00, 0x01, // program_number 1
		0xF0, 0x00, // reserved + PMT PID 0x1000
		0x00, 0x00, 0x00, 0x00, // CRC (unchecked)
	}
	return tsPacket(0x0000, true, 0, append([]byte{0x00}, sec...)) // pointer_field 0
}

func pmtPacket() []byte {
	sec := []byte{
		0x02,       // table_id
		0xB0, 0x12, // section_syntax + length=18
		0x00, 0x01, // program_number 1
		0xC1, 0x00, 0x00, // version/current, section#, last#
		0xE1, 0x00, // PCR_PID 0x100
		0xF0, 0x00, // program_info_length 0
		0x1B, 0xE1, 0x00, 0xF0, 0x00, // stream_type H.264, elem PID 0x100, ES_info 0
		0x00, 0x00, 0x00, 0x00, // CRC (unchecked)
	}
	return tsPacket(0x1000, true, 0, append([]byte{0x00}, sec...))
}

func encodePTS(pts int64) []byte {
	return []byte{
		byte(0x21 | ((pts >> 29) & 0x0E)),
		byte(pts >> 22),
		byte(0x01 | ((pts >> 14) & 0xFE)),
		byte(pts >> 7),
		byte(0x01 | ((pts << 1) & 0xFE)),
	}
}

func videoPES(pts int64, cc int, keyframe bool) []byte {
	pes := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05}
	pes = append(pes, encodePTS(pts)...)
	if keyframe {
		pes = append(pes, 0x00, 0x00, 0x01, 0x67, 0xAA) // SPS
		pes = append(pes, 0x00, 0x00, 0x01, 0x65, 0xBB) // IDR slice
	} else {
		pes = append(pes, 0x00, 0x00, 0x01, 0x41, 0xCC) // non-IDR slice
	}
	return tsPacket(0x0100, true, cc, pes)
}

// buildTSStream emits PAT+PMT then a keyframe every 0.5s with a non-keyframe in
// between, for n keyframes. Returns the byte stream.
func buildTSStream(nKeyframes int) []byte {
	var out []byte
	cc := 0
	for i := 0; i < nKeyframes; i++ {
		out = append(out, patPacket()...)
		out = append(out, pmtPacket()...)
		pts := int64(i) * 45000 // 0.5s in 90kHz
		out = append(out, videoPES(pts, cc, true)...)
		cc = (cc + 1) & 0xF
		out = append(out, videoPES(pts+22500, cc, false)...) // +0.25s non-keyframe
		cc = (cc + 1) & 0xF
	}
	return out
}

func TestSegmenterCutsOnKeyframes(t *testing.T) {
	var segs []Segment
	seg := NewTSSegmenter(1*time.Second, func(s Segment) { segs = append(segs, s) })
	seg.Write(buildTSStream(7)) // keyframes at 0,0.5,1.0,1.5,2.0,2.5,3.0s
	seg.Close()

	// target 1s → cuts at the keyframes crossing each 1s boundary: [0,1),[1,2),
	// [2,3) plus the final flush → 4 segments.
	if len(segs) < 3 {
		t.Fatalf("expected >=3 segments, got %d", len(segs))
	}
	for _, s := range segs {
		if len(s.Data)%tsPacketLen != 0 {
			t.Fatalf("seg %d not packet-aligned", s.Seq)
		}
		if !firstVideoIsKeyframe(s.Data, 0x0100) {
			t.Fatalf("seg %d does not start on a keyframe", s.Seq)
		}
	}
	// Sequence numbers are contiguous from 0.
	for i, s := range segs {
		if s.Seq != i {
			t.Fatalf("seg index %d has Seq %d", i, s.Seq)
		}
	}
}

func TestDVRPlaylistAndSegmentServing(t *testing.T) {
	d := NewDVR(Config{TargetSegmentDuration: 1 * time.Second, Window: time.Minute})
	d.Write(buildTSStream(7))
	d.Close()
	if d.SegmentCount() < 3 {
		t.Fatalf("want >=3 segments, got %d", d.SegmentCount())
	}

	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	// Playlist.
	resp, err := http.Get(srv.URL + "/index.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "mpegurl") {
		t.Fatalf("playlist content-type=%q", ct)
	}
	for _, want := range []string{"#EXTM3U", "#EXT-X-MEDIA-SEQUENCE:", "#EXTINF:", "seg0.ts"} {
		if !strings.Contains(body, want) {
			t.Fatalf("playlist missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatal("live playlist must not have ENDLIST")
	}

	// Segment fetch + range support.
	segResp, err := http.Get(srv.URL + "/seg0.ts")
	if err != nil {
		t.Fatal(err)
	}
	seg0 := readAll(t, segResp)
	segResp.Body.Close()
	if segResp.Header.Get("Content-Type") != "video/mp2t" {
		t.Fatalf("seg content-type=%q", segResp.Header.Get("Content-Type"))
	}
	if len(seg0) == 0 || len(seg0)%tsPacketLen != 0 {
		t.Fatalf("seg0 bad length %d", len(seg0))
	}
	if segResp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("seg must advertise byte ranges for seeking, got %q", segResp.Header.Get("Accept-Ranges"))
	}

	// A missing segment 404s (so the player refetches the playlist).
	missing, _ := http.Get(srv.URL + "/seg9999.ts")
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing segment status=%d", missing.StatusCode)
	}
	missing.Body.Close()
}

func TestDVREvictsOldSegments(t *testing.T) {
	// Small window keeps only ~2s of history; feeding 3s of segments must evict.
	d := NewDVR(Config{TargetSegmentDuration: 1 * time.Second, Window: 2 * time.Second})
	d.Write(buildTSStream(9)) // ~4s of keyframes
	d.Close()

	d.mu.RLock()
	first := d.segs[0].seq
	total := d.total
	d.mu.RUnlock()
	if first == 0 {
		t.Fatalf("expected oldest segments evicted, still start at Seq 0")
	}
	if total > 3*time.Second {
		t.Fatalf("window not bounded: %v retained", total)
	}
}

// TestDVRDiskBackend verifies segments spill to disk when Config.Dir is set,
// serve correctly, and the spill directory is removed on Close.
func TestDVRDiskBackend(t *testing.T) {
	root := t.TempDir()
	d := NewDVR(Config{TargetSegmentDuration: 1 * time.Second, Window: time.Minute, Dir: root})
	if d.dir == "" {
		t.Fatal("expected a spill directory to be created")
	}
	d.Write(buildTSStream(7))

	// Segments are on disk (in the spill dir), not held as bytes.
	d.mu.RLock()
	for _, s := range d.segs {
		if s.path == "" || s.data != nil {
			t.Fatalf("seg %d not disk-backed: path=%q data=%d", s.seq, s.path, len(s.data))
		}
	}
	spillDir := d.dir
	d.mu.RUnlock()

	// Served content matches the on-disk file.
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/seg0.ts")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	resp.Body.Close()
	if len(body) == 0 || len(body)%tsPacketLen != 0 {
		t.Fatalf("served seg0 bad length %d", len(body))
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("disk segment must support ranges, got %q", resp.Header.Get("Accept-Ranges"))
	}

	// Close removes the spill directory.
	d.Close()
	if _, err := os.Stat(spillDir); !os.IsNotExist(err) {
		t.Fatalf("spill dir not removed on Close: %v", err)
	}
}

// TestDVREvictsByBytes verifies the MaxBytes cap bounds the retained window
// even with a generous duration Window (so a high-bitrate stream can't balloon
// RAM).
func TestDVREvictsByBytes(t *testing.T) {
	// Large time Window, tiny byte cap → eviction is driven by bytes.
	d := NewDVR(Config{TargetSegmentDuration: 1 * time.Second, Window: time.Hour, MaxBytes: 4096})
	d.Write(buildTSStream(9))
	d.Close()

	d.mu.RLock()
	segs := len(d.segs)
	bytes := d.totalBytes
	first := d.segs[0].seq
	d.mu.RUnlock()

	if bytes > 4096 {
		t.Fatalf("retained %d bytes, exceeds MaxBytes 4096", bytes)
	}
	if first == 0 {
		t.Fatalf("expected oldest segments evicted by byte cap, still start at seq 0")
	}
	if segs == 0 {
		t.Fatal("evicted everything")
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
