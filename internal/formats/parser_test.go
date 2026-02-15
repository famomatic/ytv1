package formats

import (
	"testing"

	"github.com/famomatic/ytv1/internal/innertube"
)

func TestParse_NormalizesFormatsDeterministically(t *testing.T) {
	resp := &innertube.PlayerResponse{
		PlayabilityStatus: innertube.PlayabilityStatus{
			Status:            "OK",
			LiveStreamability: &innertube.LiveStreamability{},
		},
		StreamingData: innertube.StreamingData{
			Formats: []innertube.Format{
				{
					Itag:             18,
					URL:              "https://example.com/v.mp4",
					MimeType:         `video/mp4; codecs="avc1.42001E, mp4a.40.2"`,
					Bitrate:          500000,
					Width:            640,
					Height:           360,
					FPS:              30,
					AudioSampleRate:  "44100",
					AudioChannels:    2,
					ApproxDurationMs: "213341",
					ContentLength:    "3792299",
					InitRange:        &innertube.Range{Start: "0", End: "731"},
					IndexRange:       &innertube.Range{Start: "732", End: "1200"},
				},
			},
			AdaptiveFormats: []innertube.Format{
				{
					Itag:             251,
					MimeType:         `audio/webm; codecs="opus"`,
					SignatureCipher:  "url=https%3A%2F%2Fexample.com%2Fa.webm&sp=sig&s=encrypted",
					ApproxDurationMs: "213341",
				},
			},
		},
	}

	out := Parse(resp)
	if len(out) != 2 {
		t.Fatalf("expected 2 formats, got %d", len(out))
	}

	prog := out[0]
	if prog.Itag != 18 {
		t.Fatalf("itag mismatch: got=%d", prog.Itag)
	}
	if prog.Container != "mp4" {
		t.Fatalf("container mismatch: got=%q", prog.Container)
	}
	if prog.FPS != 30 {
		t.Fatalf("fps mismatch: got=%d", prog.FPS)
	}
	if prog.AudioSampleRate != 44100 {
		t.Fatalf("audio sample rate mismatch: got=%d", prog.AudioSampleRate)
	}
	if prog.ContentLength != 3792299 {
		t.Fatalf("content length mismatch: got=%d", prog.ContentLength)
	}
	if prog.InitRange == nil || prog.IndexRange == nil {
		t.Fatal("expected init/index ranges")
	}
	if !prog.HasAudio || !prog.HasVideo {
		t.Fatalf("expected progressive format to have both tracks: %+v", prog)
	}
	if prog.Ciphered {
		t.Fatal("expected progressive format not to be ciphered")
	}
	if !prog.ThisIsLive {
		t.Fatal("expected live flag to propagate from response")
	}

	audioOnly := out[1]
	if audioOnly.Itag != 251 {
		t.Fatalf("itag mismatch: got=%d", audioOnly.Itag)
	}
	if !audioOnly.HasAudio || audioOnly.HasVideo {
		t.Fatalf("expected audio-only adaptive format, got hasAudio=%v hasVideo=%v", audioOnly.HasAudio, audioOnly.HasVideo)
	}
	if !audioOnly.Ciphered {
		t.Fatal("expected ciphered flag to be true for signatureCipher-only format")
	}
	if audioOnly.Protocol != "https" {
		t.Fatalf("protocol mismatch: got=%q", audioOnly.Protocol)
	}
}

func TestParse_MissingAndInvalidFields(t *testing.T) {
	resp := &innertube.PlayerResponse{
		StreamingData: innertube.StreamingData{
			AdaptiveFormats: []innertube.Format{
				{
					Itag:             140,
					URL:              "",
					MimeType:         `audio/mp4; codecs="mp4a.40.2"`,
					AudioSampleRate:  "not-a-number",
					ApproxDurationMs: "bad",
					ContentLength:    "bad",
					InitRange:        &innertube.Range{Start: "bad", End: "value"},
					Cipher:           "url=https%3A%2F%2Fexample.com%2Fa.m4a&s=enc",
				},
			},
		},
	}

	out := Parse(resp)
	if len(out) != 1 {
		t.Fatalf("expected 1 format, got %d", len(out))
	}

	f := out[0]
	if f.AudioSampleRate != 0 || f.ApproxDurationMs != 0 || f.ContentLength != 0 {
		t.Fatalf("expected invalid numeric fields to normalize to zero: %+v", f)
	}
	if f.InitRange == nil {
		t.Fatal("expected init range pointer to be present")
	}
	if f.InitRange.Start != 0 || f.InitRange.End != 0 {
		t.Fatalf("expected invalid range values to normalize to zero: %+v", f.InitRange)
	}
	if !f.Ciphered {
		t.Fatal("expected ciphered flag true when url is empty and cipher is present")
	}
	if !f.HasAudio || f.HasVideo {
		t.Fatalf("expected audio-only flags from mime+codec: hasAudio=%v hasVideo=%v", f.HasAudio, f.HasVideo)
	}
}
