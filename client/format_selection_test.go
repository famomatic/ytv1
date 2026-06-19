package client

import (
	"errors"
	"testing"
)

func TestSelectFormatsForDownloadOptions_DefaultBestMergesVideoAndAudio(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 160, MimeType: "video/mp4", HasVideo: true, Height: 144, Bitrate: 120_000, Protocol: "https"},
		{Itag: 18, MimeType: "video/mp4", HasAudio: true, HasVideo: true, Height: 360, Bitrate: 700_000, Protocol: "https"},
		{Itag: 136, MimeType: "video/mp4", HasVideo: true, Height: 720, Bitrate: 1_500_000, Protocol: "https"},
		{Itag: 137, MimeType: "video/mp4", HasVideo: true, Height: 1080, Bitrate: 2_500_000, Protocol: "https"},
		{Itag: 139, MimeType: "audio/mp4", HasAudio: true, Bitrate: 48_000, Protocol: "https"},
		{Itag: 140, MimeType: "audio/mp4", HasAudio: true, Bitrate: 128_000, Protocol: "https"},
	}

	got, err := SelectFormatsForDownloadOptions(formats, DownloadOptions{Mode: SelectionModeBest})
	if err != nil {
		t.Fatalf("SelectFormatsForDownloadOptions() error = %v", err)
	}
	if len(got) != 2 || got[0].Itag != 137 || got[1].Itag != 140 {
		t.Fatalf("selected itags = %v, want [137 140]", itagsOf(got))
	}
}

func TestSelectFormatsForDownloadOptions_Itag(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 18, MimeType: "video/mp4", HasAudio: true, HasVideo: true},
		{Itag: 140, MimeType: "audio/mp4", HasAudio: true},
	}

	got, err := SelectFormatsForDownloadOptions(formats, DownloadOptions{Itag: 140, Mode: SelectionModeBest})
	if err != nil {
		t.Fatalf("SelectFormatsForDownloadOptions() error = %v", err)
	}
	if len(got) != 1 || got[0].Itag != 140 {
		t.Fatalf("selected itags = %v, want [140]", itagsOf(got))
	}
}

func TestSelectFormatsForDownloadOptions_ParseFailureIsDetailed(t *testing.T) {
	_, err := SelectFormatsForDownloadOptions(
		[]FormatInfo{{Itag: 18, MimeType: "video/mp4", HasAudio: true, HasVideo: true}},
		DownloadOptions{FormatSelector: "definitely-not-a-selector"},
	)
	var detail *NoPlayableFormatsDetailError
	if !errors.As(err, &detail) {
		t.Fatalf("error = %T %v, want NoPlayableFormatsDetailError", err, err)
	}
	if detail.Selector != "definitely-not-a-selector" || detail.SelectionError == "" {
		t.Fatalf("detail = %+v, want selector and selection error", detail)
	}
}

func TestSelectFormatsForDownloadOptions_PrefersNonCipheredAlternative(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 137, MimeType: "video/mp4", HasVideo: true, Height: 1080, Ciphered: true, Protocol: "https"},
		{Itag: 136, MimeType: "video/mp4", HasVideo: true, Height: 720, Protocol: "https"},
		{Itag: 140, MimeType: "audio/mp4", HasAudio: true, Bitrate: 128_000, Protocol: "https"},
	}

	got, err := SelectFormatsForDownloadOptions(formats, DownloadOptions{Mode: SelectionModeBest})
	if err != nil {
		t.Fatalf("SelectFormatsForDownloadOptions() error = %v", err)
	}
	if len(got) != 2 || got[0].Itag != 136 || got[1].Itag != 140 {
		t.Fatalf("selected itags = %v, want [136 140]", itagsOf(got))
	}
}

func TestSelectFormatsForDownloadOptions_ExcludesDamagedMetadataOnlyFormats(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 315, MimeType: "video/webm", HasVideo: true, Height: 2160, Bitrate: 26_000_000, Protocol: "unknown", IsDamaged: true},
		{Itag: 160, MimeType: "video/mp4", HasVideo: true, Height: 144, Bitrate: 120_000, Protocol: "https"},
		{Itag: 299, MimeType: "video/mp4", HasVideo: true, Height: 1080, Bitrate: 4_200_000, Protocol: "https"},
		{Itag: 251, MimeType: "audio/webm", HasAudio: true, Bitrate: 146_000, Protocol: "https"},
	}

	got, err := SelectFormatsForDownloadOptions(formats, DownloadOptions{Mode: SelectionModeBest})
	if err != nil {
		t.Fatalf("SelectFormatsForDownloadOptions() error = %v", err)
	}
	if len(got) != 2 || got[0].Itag != 299 || got[1].Itag != 251 {
		t.Fatalf("selected itags = %v, want [299 251]", itagsOf(got))
	}
}

func itagsOf(formats []FormatInfo) []int {
	itags := make([]int, 0, len(formats))
	for _, format := range formats {
		itags = append(itags, format.Itag)
	}
	return itags
}
