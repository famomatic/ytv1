package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
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
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/s/player/"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`var cfg={signatureTimestamp:20494};`)),
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
		"videoDetails":{
			"videoId":"jNQXAC9IVRw",
			"title":"Me at the zoo",
			"author":"jawed",
			"shortDescription":"hello world",
			"lengthSeconds":"19",
			"viewCount":"12345",
			"channelId":"UC4QobU6STFB0P71PMvOGN5A",
			"isLiveContent":false,
			"keywords":["zoo","classic"]
		},
		"microformat":{
			"playerMicroformatRenderer":{
				"publishDate":"2005-04-23",
				"uploadDate":"2005-04-23",
				"category":"Pets & Animals"
			}
		},
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
	if info.Description != "hello world" {
		t.Fatalf("description = %q", info.Description)
	}
	if info.DurationSec != 19 {
		t.Fatalf("duration = %d, want 19", info.DurationSec)
	}
	if info.ViewCount != 12345 {
		t.Fatalf("view count = %d, want 12345", info.ViewCount)
	}
	if info.ChannelID != "UC4QobU6STFB0P71PMvOGN5A" {
		t.Fatalf("channel id = %q", info.ChannelID)
	}
	if info.PublishDate != "2005-04-23" || info.UploadDate != "2005-04-23" {
		t.Fatalf("unexpected dates: publish=%q upload=%q", info.PublishDate, info.UploadDate)
	}
	if info.Category != "Pets & Animals" {
		t.Fatalf("category = %q", info.Category)
	}
	if len(info.Keywords) != 2 {
		t.Fatalf("keywords len = %d, want 2", len(info.Keywords))
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

func TestGetVideoEmitsExtractionEventsForWebpageAndManifest(t *testing.T) {
	playerJSON := `{
		"playabilityStatus":{"status":"OK"},
		"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
		"streamingData":{
			"formats":[{"itag":18,"url":"https://example.com/v.mp4","mimeType":"video/mp4","bitrate":1000}],
			"dashManifestUrl":"https://example.com/manifest.mpd",
			"hlsManifestUrl":"https://example.com/manifest.m3u8"
		}
	}`

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
				html := `<html><script src="/s/player/1798f86c/player_es6.vflset/ko_KR/base.js"></script></html>`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(html)),
				}, nil
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/s/player/"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`var cfg={signatureTimestamp:20494};`)),
				}, nil
			case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, ".mpd"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"></MPD>`)),
				}, nil
			case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, ".m3u8"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`#EXTM3U`)),
				}, nil
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				return nil, nil
			}
		}),
	}

	var mu sync.Mutex
	var events []ExtractionEvent
	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		OnExtractionEvent: func(evt ExtractionEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, evt)
		},
	})

	_, err := c.GetVideo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideo() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var hasWebpageStart, hasWebpageSuccess, hasManifestStart bool
	for _, evt := range events {
		if evt.Stage == "webpage" && evt.Phase == "start" {
			hasWebpageStart = true
		}
		if evt.Stage == "webpage" && evt.Phase == "success" {
			hasWebpageSuccess = true
		}
		if evt.Stage == "manifest" && evt.Phase == "start" {
			hasManifestStart = true
		}
	}
	if !hasWebpageStart || !hasWebpageSuccess {
		t.Fatalf("expected webpage start/success events, got=%v", events)
	}
	if !hasManifestStart {
		t.Fatalf("expected manifest start event, got=%v", events)
	}
}
