package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestResolveStreamURL_DegradesWhenNChallengeDecodeFails(t *testing.T) {
	var events []ExtractionEvent
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(bytes.NewBufferString(`{
						"playabilityStatus":{"status":"OK"},
						"videoDetails":{"videoId":"jNQXAC9IVRw","title":"Me at the zoo","author":"jawed"},
						"streamingData":{"formats":[{"itag":18,"url":"https://media.local/v18.mp4?n=abcd","mimeType":"video/mp4","bitrate":1000}]}
					}`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<html><script src="/s/player/test/player_ias.vflset/en_US/base.js"></script></html>`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/s/player/test/player_ias.vflset/en_US/base.js":
				// Intentionally invalid player JS to force n-decode failure.
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`var broken = true;`)),
				}, nil
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				return nil, nil
			}
		}),
	}
	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		OnExtractionEvent: func(evt ExtractionEvent) {
			events = append(events, evt)
		},
	})

	streamURL, err := c.ResolveStreamURL(context.Background(), "jNQXAC9IVRw", 18)
	if err != nil {
		t.Fatalf("ResolveStreamURL() error = %v", err)
	}
	if streamURL != "https://media.local/v18.mp4?n=abcd" {
		t.Fatalf("streamURL=%q, want unchanged n url", streamURL)
	}

	foundPartial := false
	for _, evt := range events {
		if evt.Stage == "challenge" && evt.Phase == "partial" {
			foundPartial = strings.Contains(evt.Detail, "n=1")
		}
	}
	if !foundPartial {
		t.Fatalf("expected challenge partial event with n failure detail, events=%v", events)
	}
}
