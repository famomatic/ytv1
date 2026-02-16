package selector

import (
	"testing"

	"github.com/famomatic/ytv1/internal/types"
)

func TestSelect_ExtM4AMergeRecipe(t *testing.T) {
	formats := []types.FormatInfo{
		{Itag: 137, MimeType: `video/mp4; codecs="avc1"`, HasVideo: true, Width: 1920, Height: 1080, FPS: 30, Bitrate: 4_000_000},
		{Itag: 140, MimeType: `audio/mp4; codecs="mp4a"`, HasAudio: true, Bitrate: 128_000},
		{Itag: 251, MimeType: `audio/webm; codecs="opus"`, HasAudio: true, Bitrate: 160_000},
	}

	sel, err := Parse("bestvideo[ext=mp4]+bestaudio[ext=m4a]")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got, err := Select(formats, sel)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(selected) = %d, want 2", len(got))
	}
	if got[0].Itag != 137 || got[1].Itag != 140 {
		t.Fatalf("selected itags = [%d,%d], want [137,140]", got[0].Itag, got[1].Itag)
	}
}

func TestSelect_FallbackToBestExtMP4(t *testing.T) {
	formats := []types.FormatInfo{
		{Itag: 137, MimeType: `video/mp4; codecs="avc1"`, HasVideo: true, Width: 1920, Height: 1080, FPS: 30, Bitrate: 4_000_000},
		{Itag: 251, MimeType: `audio/webm; codecs="opus"`, HasAudio: true, Bitrate: 160_000},
		{Itag: 22, MimeType: `video/mp4; codecs="avc1,mp4a"`, HasVideo: true, HasAudio: true, Width: 1280, Height: 720, FPS: 30, Bitrate: 2_000_000},
	}

	sel, err := Parse("bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got, err := Select(formats, sel)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(selected) = %d, want 1", len(got))
	}
	if got[0].Itag != 22 {
		t.Fatalf("selected itag = %d, want 22", got[0].Itag)
	}
}

func TestSelect_WidthFilter(t *testing.T) {
	formats := []types.FormatInfo{
		{Itag: 136, MimeType: `video/mp4; codecs="avc1"`, HasVideo: true, Width: 1280, Height: 720, FPS: 30, Bitrate: 2_000_000},
		{Itag: 137, MimeType: `video/mp4; codecs="avc1"`, HasVideo: true, Width: 1920, Height: 1080, FPS: 30, Bitrate: 4_000_000},
	}

	sel, err := Parse("bestvideo[width>=1920]")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got, err := Select(formats, sel)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(selected) = %d, want 1", len(got))
	}
	if got[0].Itag != 137 {
		t.Fatalf("selected itag = %d, want 137", got[0].Itag)
	}
}

func TestSelect_WorstAudioAlias(t *testing.T) {
	formats := []types.FormatInfo{
		{Itag: 140, MimeType: `audio/mp4; codecs="mp4a"`, HasAudio: true, Bitrate: 128_000},
		{Itag: 251, MimeType: `audio/webm; codecs="opus"`, HasAudio: true, Bitrate: 160_000},
	}

	sel, err := Parse("worstaudio")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got, err := Select(formats, sel)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(selected) = %d, want 1", len(got))
	}
	if got[0].Itag != 140 {
		t.Fatalf("selected itag = %d, want 140", got[0].Itag)
	}
}

func TestSelect_WorstVideoAlias(t *testing.T) {
	formats := []types.FormatInfo{
		{Itag: 136, MimeType: `video/mp4; codecs="avc1"`, HasVideo: true, Width: 1280, Height: 720, FPS: 30, Bitrate: 2_000_000},
		{Itag: 137, MimeType: `video/mp4; codecs="avc1"`, HasVideo: true, Width: 1920, Height: 1080, FPS: 30, Bitrate: 4_000_000},
	}

	sel, err := Parse("worstvideo")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got, err := Select(formats, sel)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(selected) = %d, want 1", len(got))
	}
	if got[0].Itag != 136 {
		t.Fatalf("selected itag = %d, want 136", got[0].Itag)
	}
}

func TestSelect_FPSNotEqualFilter(t *testing.T) {
	formats := []types.FormatInfo{
		{Itag: 299, MimeType: `video/mp4; codecs="avc1"`, HasVideo: true, Width: 1920, Height: 1080, FPS: 60, Bitrate: 5_000_000},
		{Itag: 137, MimeType: `video/mp4; codecs="avc1"`, HasVideo: true, Width: 1920, Height: 1080, FPS: 30, Bitrate: 4_000_000},
	}

	sel, err := Parse("bestvideo[fps!=60]")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got, err := Select(formats, sel)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(selected) = %d, want 1", len(got))
	}
	if got[0].Itag != 137 {
		t.Fatalf("selected itag = %d, want 137", got[0].Itag)
	}
}
