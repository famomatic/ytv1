package client

import "testing"

func TestPredictOutputFilename_MergedTemplate(t *testing.T) {
	info := &VideoInfo{
		ID:        "video123",
		Title:     "Title/Bad",
		Author:    "Uploader",
		ChannelID: "UC123",
	}
	formats := []FormatInfo{
		{Itag: 248, MimeType: "video/webm; codecs=\"vp9\"", HasVideo: true, Width: 1920, Height: 1080, Bitrate: 2000000},
		{Itag: 251, MimeType: "audio/webm; codecs=\"opus\"", HasAudio: true, Bitrate: 128000},
	}
	got, err := PredictOutputFilename(info, formats, OutputFilenameOptions{
		OutputTemplate: "%(title)s-%(format_id)s-%(resolution)s.%(ext)s",
		MergeOutputExt: "mkv",
	})
	if err != nil {
		t.Fatalf("PredictOutputFilename() error = %v", err)
	}
	want := "Title_Bad-248+251-1920x1080.mkv"
	if got != want {
		t.Fatalf("PredictOutputFilename()=%q, want %q", got, want)
	}
}

func TestPredictOutputFilename_DefaultSingleAndTrim(t *testing.T) {
	info := &VideoInfo{ID: "video123", Title: "Video title"}
	formats := []FormatInfo{{Itag: 140, MimeType: "audio/mp4", HasAudio: true}}
	got, err := PredictOutputFilename(info, formats, OutputFilenameOptions{})
	if err != nil {
		t.Fatalf("PredictOutputFilename() error = %v", err)
	}
	if got != "Video title [video123].mp4" {
		t.Fatalf("PredictOutputFilename()=%q, want Video title [video123].mp4", got)
	}
}

func TestPredictOutputFilename_RestrictedSingleTemplate(t *testing.T) {
	info := &VideoInfo{ID: "video123", Title: "A title 한글 / symbol"}
	formats := []FormatInfo{{Itag: 18, MimeType: "video/mp4", HasVideo: true, HasAudio: true}}
	got, err := PredictOutputFilename(info, formats, OutputFilenameOptions{
		OutputTemplate:    "%(title)s.%(ext)s",
		RestrictFilenames: true,
	})
	if err != nil {
		t.Fatalf("PredictOutputFilename() error = %v", err)
	}
	if got != "A_title_symbol.mp4" {
		t.Fatalf("PredictOutputFilename()=%q, want A_title_symbol.mp4", got)
	}
}

func TestEffectiveMergeOutputExt(t *testing.T) {
	if got := EffectiveMergeOutputExt("mkv", "webm>mp4"); got != "mkv" {
		t.Fatalf("EffectiveMergeOutputExt()=%q, want mkv", got)
	}
	if got := EffectiveMergeOutputExt("", "webm>mp4/mkv"); got != "mp4" {
		t.Fatalf("EffectiveMergeOutputExt(remux)=%q, want mp4", got)
	}
	if got := EffectiveMergeOutputExt("", ""); got != "mp4" {
		t.Fatalf("EffectiveMergeOutputExt(default)=%q, want mp4", got)
	}
}

// TestPredictOutputFilename_WebMDefaultNoFFmpeg verifies that a WebM
// video+audio pair predicts a .webm merge container by default (no explicit
// MergeOutputExt), so the pure-Go puremux muxer can handle it without ffmpeg.
func TestPredictOutputFilename_WebMDefaultNoFFmpeg(t *testing.T) {
	info := &VideoInfo{ID: "video123", Title: "Some title"}
	formats := []FormatInfo{
		{Itag: 248, MimeType: "video/webm; codecs=\"vp9\"", HasVideo: true, Width: 1920, Height: 1080, Bitrate: 2000000},
		{Itag: 251, MimeType: "audio/webm; codecs=\"opus\"", HasAudio: true, Bitrate: 128000},
	}
	got, err := PredictOutputFilename(info, formats, OutputFilenameOptions{})
	if err != nil {
		t.Fatalf("PredictOutputFilename() error = %v", err)
	}
	want := "Some title [video123].webm"
	if got != want {
		t.Fatalf("PredictOutputFilename()=%q, want %q (webm default for WebM pairs)", got, want)
	}
}

// TestPredictOutputFilename_MP4PairStaysMP4 verifies that a non-WebM (MP4)
// video+audio pair still predicts .mp4 by default, preserving broad
// compatibility and falling back to ffmpeg.
func TestPredictOutputFilename_MP4PairStaysMP4(t *testing.T) {
	info := &VideoInfo{ID: "video123", Title: "Some title"}
	formats := []FormatInfo{
		{Itag: 299, MimeType: "video/mp4; codecs=\"avc1\"", HasVideo: true, Width: 1920, Height: 1080, Bitrate: 2000000},
		{Itag: 140, MimeType: "audio/mp4; codecs=\"mp4a\"", HasAudio: true, Bitrate: 128000},
	}
	got, err := PredictOutputFilename(info, formats, OutputFilenameOptions{})
	if err != nil {
		t.Fatalf("PredictOutputFilename() error = %v", err)
	}
	want := "Some title [video123].mp4"
	if got != want {
		t.Fatalf("PredictOutputFilename()=%q, want %q (mp4 default for MP4 pairs)", got, want)
	}
}
