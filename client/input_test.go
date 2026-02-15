package client

import (
	"errors"
	"testing"
)

func TestExtractVideoID_SupportedShapes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "jNQXAC9IVRw", want: "jNQXAC9IVRw"},
		{in: "https://www.youtube.com/watch?v=jNQXAC9IVRw", want: "jNQXAC9IVRw"},
		{in: "https://m.youtube.com/watch?v=jNQXAC9IVRw&pp=ygU=", want: "jNQXAC9IVRw"},
		{in: "https://youtu.be/jNQXAC9IVRw?t=1", want: "jNQXAC9IVRw"},
		{in: "youtube.com/watch?v=jNQXAC9IVRw", want: "jNQXAC9IVRw"},
		{in: "https://www.youtube.com/embed/jNQXAC9IVRw", want: "jNQXAC9IVRw"},
		{in: "https://www.youtube.com/v/jNQXAC9IVRw", want: "jNQXAC9IVRw"},
		{in: "https://www.youtube.com/shorts/jNQXAC9IVRw", want: "jNQXAC9IVRw"},
		{in: "https://www.youtube.com/live/jNQXAC9IVRw", want: "jNQXAC9IVRw"},
	}
	for _, tt := range tests {
		got, err := ExtractVideoID(tt.in)
		if err != nil {
			t.Fatalf("ExtractVideoID(%q) error=%v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("ExtractVideoID(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractVideoID_InvalidDetailReason(t *testing.T) {
	_, err := ExtractVideoID("https://example.com/watch?v=jNQXAC9IVRw")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	var detail *InvalidInputDetailError
	if !errors.As(err, &detail) {
		t.Fatalf("expected InvalidInputDetailError, got %T", err)
	}
	if detail.Reason != "unsupported_host" {
		t.Fatalf("reason=%q, want %q", detail.Reason, "unsupported_host")
	}
}

func TestExtractPlaylistID_SupportedShapes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "PLabc123", want: "PLabc123"},
		{in: "https://www.youtube.com/playlist?list=PLabc123", want: "PLabc123"},
		{in: "https://www.youtube.com/watch?v=jNQXAC9IVRw&list=PLabc123", want: "PLabc123"},
		{in: "youtube.com/watch?v=jNQXAC9IVRw&list=PLabc123", want: "PLabc123"},
	}
	for _, tt := range tests {
		got, err := ExtractPlaylistID(tt.in)
		if err != nil {
			t.Fatalf("ExtractPlaylistID(%q) error=%v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("ExtractPlaylistID(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractPlaylistID_MissingList(t *testing.T) {
	_, err := ExtractPlaylistID("https://www.youtube.com/watch?v=jNQXAC9IVRw")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	var detail *InvalidInputDetailError
	if !errors.As(err, &detail) {
		t.Fatalf("expected InvalidInputDetailError, got %T", err)
	}
	if detail.Reason != "missing_playlist_id" {
		t.Fatalf("reason=%q, want %q", detail.Reason, "missing_playlist_id")
	}
}
