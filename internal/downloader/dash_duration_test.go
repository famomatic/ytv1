package downloader

import (
	"fmt"
	"testing"
	"time"
)

func TestParseISO8601Duration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"PT1S", 1 * time.Second},
		{"PT1H30M", time.Hour + 30*time.Minute},
		{"PT0S", 0},
		{"PT2.5S", 2500 * time.Millisecond},
		{"PT1H", time.Hour},
		{"P1DT2H", 26 * time.Hour},
		{"PT1M30S", 90 * time.Second},
	}
	for _, c := range cases {
		got, err := parseDuration(c.in)
		if err != nil {
			t.Errorf("parseDuration(%q) error = %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseISO8601Duration_Invalid(t *testing.T) {
	for _, in := range []string{"", "abc", "P", "PT"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q) expected error, got nil", in)
		}
	}
}

// TestExtractSegments_RepeatMinusOne_StaticManifest verifies that r=-1 in
// a static DASH manifest is expanded using mediaPresentationDuration so the
// full stream is downloaded instead of a single segment.
func TestExtractSegments_RepeatMinusOne_StaticManifest(t *testing.T) {
	// Build a static MPD with a single S entry that has r=-1.
	// Segment duration d=1000000 (timescale=1000000 => 1 second per segment).
	// mediaPresentationDuration = PT10S => expect 10 segments.
	mpdXML := `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT10S" minBufferTime="PT1S">
  <Period>
    <AdaptationSet mimeType="video/mp4">
      <Representation id="999" bandwidth="1000000">
        <SegmentTemplate timescale="1000000" media="seg-$Number$.m4s" startNumber="1" initialization="init.m4s">
          <SegmentTimeline>
            <S t="0" d="1000000" r="-1"/>
          </SegmentTimeline>
        </SegmentTemplate>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

	mpd, err := parseDASH([]byte(mpdXML))
	if err != nil {
		t.Fatalf("parseDASH() error = %v", err)
	}

	d := NewDASHDownloader(nil, "https://example.com/manifest.mpd", "999")
	segments, _, err := d.extractSegments(mpd)
	if err != nil {
		t.Fatalf("extractSegments() error = %v", err)
	}

	if len(segments) != 10 {
		t.Fatalf("expected 10 segments for r=-1 with 10s duration, got %d", len(segments))
	}

	// Verify segment numbers are sequential.
	for i, seg := range segments {
		expectedSeq := int64(i + 1)
		if seg.Seq != expectedSeq {
			t.Errorf("segment[%d].Seq = %d, want %d", i, seg.Seq, expectedSeq)
		}
	}
}

// TestExtractSegments_RepeatMinusOne_DynamicManifest verifies that r=-1 in
// a dynamic manifest generates a single segment (the caller re-fetches).
func TestExtractSegments_RepeatMinusOne_DynamicManifest(t *testing.T) {
	mpdXML := `<?xml version="1.0"?>
<MPD type="dynamic" minimumUpdatePeriod="PT2S" minBufferTime="PT1S">
  <Period>
    <AdaptationSet mimeType="video/mp4">
      <Representation id="999" bandwidth="1000000">
        <SegmentTemplate timescale="1000000" media="seg-$Number$.m4s" startNumber="1" initialization="init.m4s">
          <SegmentTimeline>
            <S t="0" d="1000000" r="-1"/>
          </SegmentTimeline>
        </SegmentTemplate>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

	mpd, err := parseDASH([]byte(mpdXML))
	if err != nil {
		t.Fatalf("parseDASH() error = %v", err)
	}

	d := NewDASHDownloader(nil, "https://example.com/manifest.mpd", "999")
	segments, _, err := d.extractSegments(mpd)
	if err != nil {
		t.Fatalf("extractSegments() error = %v", err)
	}

	// Dynamic manifest: r=-1 generates a single segment; caller re-fetches.
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment for dynamic r=-1, got %d", len(segments))
	}
}

// TestExtractSegments_RepeatValue verifies normal r values expand correctly.
func TestExtractSegments_RepeatValue(t *testing.T) {
	mpdXML := `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT5S" minBufferTime="PT1S">
  <Period>
    <AdaptationSet mimeType="video/mp4">
      <Representation id="999" bandwidth="1000000">
        <SegmentTemplate timescale="1000000" media="seg-$Number$.m4s" startNumber="1" initialization="init.m4s">
          <SegmentTimeline>
            <S t="0" d="1000000" r="4"/>
          </SegmentTimeline>
        </SegmentTemplate>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

	mpd, err := parseDASH([]byte(mpdXML))
	if err != nil {
		t.Fatalf("parseDASH() error = %v", err)
	}

	d := NewDASHDownloader(nil, "https://example.com/manifest.mpd", "999")
	segments, _, err := d.extractSegments(mpd)
	if err != nil {
		t.Fatalf("extractSegments() error = %v", err)
	}

	// r=4 means 5 segments total (1 + 4 repeats).
	if len(segments) != 5 {
		t.Fatalf("expected 5 segments for r=4, got %d", len(segments))
	}

	// Suppress unused fmt import warning for future test additions.
	_ = fmt.Sprint
}

