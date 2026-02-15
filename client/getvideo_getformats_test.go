package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newMockClientForPlayerJSON(t *testing.T, playerJSON string) *Client {
	t.Helper()
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(playerJSON)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				// playerjs resolver uses /watch HTML to extract /s/player/.../base.js
				html := `<html><script src="/s/player/1798f86c/player_es6.vflset/ko_KR/base.js"></script></html>`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(html)),
				}, nil
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				return nil, nil
			}
		}),
	}

	return New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
	})
}

func TestGetVideoOK(t *testing.T) {
	c := newMockClientForPlayerJSON(t, `{
		"playabilityStatus":{"status":"OK"},
		"videoDetails":{"videoId":"jNQXAC9IVRw","title":"Me at the zoo","author":"jawed"},
		"streamingData":{"formats":[{"itag":18,"url":"https://example.com/v.mp4","mimeType":"video/mp4","bitrate":1000}]}
	}`)

	info, err := c.GetVideo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideo() error = %v", err)
	}
	if info.Title != "Me at the zoo" {
		t.Fatalf("title = %q", info.Title)
	}
	if len(info.Formats) != 1 {
		t.Fatalf("formats len = %d, want 1", len(info.Formats))
	}
}

func TestGetVideoLoginRequired(t *testing.T) {
	c := newMockClientForPlayerJSON(t, `{
		"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"Sign in to confirm your age"},
		"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"}
	}`)

	_, err := c.GetVideo(context.Background(), "jNQXAC9IVRw")
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("GetVideo() error = %v, want %v", err, ErrLoginRequired)
	}
}

func TestGetVideoUnavailable(t *testing.T) {
	c := newMockClientForPlayerJSON(t, `{
		"playabilityStatus":{"status":"UNPLAYABLE","reason":"This video is unavailable"},
		"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"}
	}`)

	_, err := c.GetVideo(context.Background(), "jNQXAC9IVRw")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("GetVideo() error = %v, want %v", err, ErrUnavailable)
	}
}

func TestGetFormatsNoPlayable(t *testing.T) {
	c := newMockClientForPlayerJSON(t, `{
		"playabilityStatus":{"status":"OK"},
		"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
		"streamingData":{"formats":[],"adaptiveFormats":[]}
	}`)

	_, err := c.GetFormats(context.Background(), "jNQXAC9IVRw")
	if !errors.Is(err, ErrNoPlayableFormats) {
		t.Fatalf("GetFormats() error = %v, want %v", err, ErrNoPlayableFormats)
	}
}
