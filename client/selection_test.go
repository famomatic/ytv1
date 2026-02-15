package client

import "testing"

func TestSelectDownloadFormat_ModeBest(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 251, MimeType: "audio/webm", HasAudio: true, Bitrate: 128000},
		{Itag: 137, MimeType: "video/mp4", HasVideo: true, Height: 1080, FPS: 30, Bitrate: 2500000},
		{Itag: 22, MimeType: "video/mp4", HasAudio: true, HasVideo: true, Height: 720, FPS: 30, Bitrate: 1800000},
	}

	got, ok := selectDownloadFormat(formats, DownloadOptions{Mode: SelectionModeBest})
	if !ok {
		t.Fatal("selectDownloadFormat() not found")
	}
	if got.Itag != 22 {
		t.Fatalf("best mode selected itag=%d, want 22", got.Itag)
	}
}

func TestSelectDownloadFormat_ModeMP4VideoOnly(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 137, MimeType: "video/mp4", HasVideo: true, Height: 1080, FPS: 30, Bitrate: 2500000},
		{Itag: 299, MimeType: "video/mp4", HasVideo: true, Height: 1080, FPS: 60, Bitrate: 3500000},
		{Itag: 248, MimeType: "video/webm", HasVideo: true, Height: 1080, FPS: 60, Bitrate: 3000000},
	}

	got, ok := selectDownloadFormat(formats, DownloadOptions{Mode: SelectionModeMP4VideoOnly})
	if !ok {
		t.Fatal("selectDownloadFormat() not found")
	}
	if got.Itag != 299 {
		t.Fatalf("mp4videoonly mode selected itag=%d, want 299", got.Itag)
	}
}

func TestSelectDownloadFormat_ModeAudioOnly(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 140, MimeType: "audio/mp4", HasAudio: true, Bitrate: 128000},
		{Itag: 251, MimeType: "audio/webm", HasAudio: true, Bitrate: 160000},
		{Itag: 22, MimeType: "video/mp4", HasAudio: true, HasVideo: true, Height: 720, Bitrate: 1800000},
	}

	got, ok := selectDownloadFormat(formats, DownloadOptions{Mode: SelectionModeAudioOnly})
	if !ok {
		t.Fatal("selectDownloadFormat() not found")
	}
	if got.Itag != 251 {
		t.Fatalf("audioonly mode selected itag=%d, want 251", got.Itag)
	}
}

func TestSelectDownloadFormat_ItagOverridePriority(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 140, MimeType: "audio/mp4", HasAudio: true, Bitrate: 128000},
		{Itag: 22, MimeType: "video/mp4", HasAudio: true, HasVideo: true, Height: 720, Bitrate: 1800000},
	}

	got, ok := selectDownloadFormat(formats, DownloadOptions{Itag: 140, Mode: SelectionModeBest})
	if !ok {
		t.Fatal("selectDownloadFormat() not found")
	}
	if got.Itag != 140 {
		t.Fatalf("itag override selected itag=%d, want 140", got.Itag)
	}
}

func TestSelectDownloadFormat_NoModeMatch(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 251, MimeType: "audio/webm", HasAudio: true, Bitrate: 160000},
	}

	_, ok := selectDownloadFormat(formats, DownloadOptions{Mode: SelectionModeMP4AV})
	if ok {
		t.Fatal("selectDownloadFormat() found format, want none")
	}
}
