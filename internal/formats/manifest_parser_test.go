package formats

import "testing"

func TestParseDASHManifest_BasicRepresentations(t *testing.T) {
	raw := `<?xml version="1.0" encoding="UTF-8"?>
<MPD>
  <Period>
    <AdaptationSet mimeType="audio/mp4" codecs="mp4a.40.2">
      <Representation id="140" bandwidth="128000" audioSamplingRate="44100">
        <BaseURL>https://cdn.example.test/audio/140.m4a</BaseURL>
      </Representation>
    </AdaptationSet>
    <AdaptationSet mimeType="video/mp4" codecs="avc1.64001f">
      <Representation id="137" bandwidth="2500000" width="1920" height="1080" frameRate="30">
        <BaseURL>https://cdn.example.test/video/137.mp4</BaseURL>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

	out, err := ParseDASHManifest(raw, "https://example.test/manifest.mpd")
	if err != nil {
		t.Fatalf("ParseDASHManifest() error = %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out)=%d, want 2", len(out))
	}
	if out[0].Protocol != "dash" || out[1].Protocol != "dash" {
		t.Fatalf("expected dash protocol for all formats: %+v", out)
	}
	if !out[0].HasAudio || out[0].HasVideo {
		t.Fatalf("audio representation flags mismatch: %+v", out[0])
	}
	if !out[1].HasVideo {
		t.Fatalf("video representation flags mismatch: %+v", out[1])
	}
}

func TestParseHLSManifest_MasterPlaylist(t *testing.T) {
	raw := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,AVERAGE-BANDWIDTH=700000,RESOLUTION=1280x720,FRAME-RATE=29.97,CODECS="avc1.4d401f,mp4a.40.2"
v/itag/22/prog.m3u8
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud1",NAME="English",DEFAULT=YES,AUTOSELECT=YES,URI="a/itag/140/audio.m3u8"
`
	out, err := ParseHLSManifest(raw, "https://example.test/master.m3u8")
	if err != nil {
		t.Fatalf("ParseHLSManifest() error = %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out)=%d, want 2", len(out))
	}
	if out[0].Protocol != "hls" || out[1].Protocol != "hls" {
		t.Fatalf("expected hls protocol: %+v", out)
	}
	if out[0].Itag != 22 || out[1].Itag != 140 {
		t.Fatalf("itag extraction mismatch: %+v", out)
	}
}

