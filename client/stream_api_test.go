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

func TestOpenFormatStream(t *testing.T) {
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
						"streamingData":{"formats":[{"itag":18,"url":"https://stream.local/v18.mp4","mimeType":"video/mp4","bitrate":1000}]}
					}`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<html><script src="/s/player/test/base.js"></script></html>`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Host == "stream.local":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString("stream-body")),
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
	})

	rc, format, err := c.OpenFormatStream(context.Background(), "jNQXAC9IVRw", 18)
	if err != nil {
		t.Fatalf("OpenFormatStream() error = %v", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if string(raw) != "stream-body" {
		t.Fatalf("unexpected body: %q", string(raw))
	}
	if format.Itag != 18 {
		t.Fatalf("selected itag = %d, want 18", format.Itag)
	}
}

func TestOpenFormatStream_NoPlayableFormat(t *testing.T) {
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
						"streamingData":{"formats":[{"itag":18,"url":"https://stream.local/v18.mp4","mimeType":"video/mp4","bitrate":1000}]}
					}`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<html><script src="/s/player/test/base.js"></script></html>`)),
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
	})

	_, _, err := c.OpenFormatStream(context.Background(), "jNQXAC9IVRw", 999)
	if !errors.Is(err, ErrNoPlayableFormats) {
		t.Fatalf("OpenFormatStream() error = %v, want %v", err, ErrNoPlayableFormats)
	}
}
