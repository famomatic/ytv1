package client

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDownload_SelectorParseFailureReturnsNoPlayableDetail(t *testing.T) {
	c := newMockClientForPlayerJSON(t, `{
		"playabilityStatus":{"status":"OK"},
		"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
		"streamingData":{"formats":[{"itag":18,"url":"https://example.com/v.mp4","mimeType":"video/mp4","bitrate":1000}]}
	}`)

	_, err := c.Download(context.Background(), "jNQXAC9IVRw", DownloadOptions{
		FormatSelector: "definitely-not-a-selector",
	})
	if !errors.Is(err, ErrNoPlayableFormats) {
		t.Fatalf("Download() error = %v, want %v", err, ErrNoPlayableFormats)
	}
	var detail *NoPlayableFormatsDetailError
	if !errors.As(err, &detail) {
		t.Fatalf("expected NoPlayableFormatsDetailError, got %T", err)
	}
	if detail.Selector != "definitely-not-a-selector" {
		t.Fatalf("selector = %q", detail.Selector)
	}
	if !strings.Contains(detail.SelectionError, "selector parse failed") {
		t.Fatalf("selection error = %q", detail.SelectionError)
	}
}

func TestDownload_SelectorNoMatchReturnsNoPlayableDetail(t *testing.T) {
	c := newMockClientForPlayerJSON(t, `{
		"playabilityStatus":{"status":"OK"},
		"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
		"streamingData":{"formats":[{"itag":18,"url":"https://example.com/v.mp4","mimeType":"video/mp4","bitrate":1000}]}
	}`)

	_, err := c.Download(context.Background(), "jNQXAC9IVRw", DownloadOptions{
		FormatSelector: "bestvideo+bestaudio",
	})
	if !errors.Is(err, ErrNoPlayableFormats) {
		t.Fatalf("Download() error = %v, want %v", err, ErrNoPlayableFormats)
	}
	var detail *NoPlayableFormatsDetailError
	if !errors.As(err, &detail) {
		t.Fatalf("expected NoPlayableFormatsDetailError, got %T", err)
	}
	if detail.Selector != "bestvideo+bestaudio" {
		t.Fatalf("selector = %q", detail.Selector)
	}
	if detail.SelectionError != "no formats matched selector" {
		t.Fatalf("selection error = %q", detail.SelectionError)
	}
}
