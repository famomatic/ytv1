package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type streamingBody struct {
	chunks [][]byte
	idx    int
}

func (s *streamingBody) Read(p []byte) (int, error) {
	if s.idx >= len(s.chunks) {
		return 0, io.EOF
	}
	n := copy(p, s.chunks[s.idx])
	s.idx++
	return n, nil
}

const playerJSONStreamBody = `{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"Me at the zoo","author":"jawed"},"streamingData":{"formats":[{"itag":18,"url":"https://stream.local/v18.mp4","mimeType":"video/mp4","bitrate":1000}]}}`

func TestOpenStream_BodyReadableAfterReturn(t *testing.T) {
	want := "stream-body-survives-return"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(playerJSONStreamBody)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<html><script src="/s/player/test/base.js"></script></html>`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Host == "stream.local":
				body := &streamingBody{chunks: [][]byte{[]byte(want)}}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(body),
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

	rc, _, err := c.OpenFormatStream(context.Background(), "jNQXAC9IVRw", 18)
	if err != nil {
		t.Fatalf("OpenFormatStream() error = %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stream after OpenStream returned: %v", err)
	}
	if string(got) != want {
		t.Fatalf("body mismatch: got %q want %q", string(got), want)
	}
}

func TestOpenStream_BodyCloseReleasesContext(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(playerJSONStreamBody)),
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
					Body:       io.NopCloser(strings.NewReader("ok")),
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

	rc, _, err := c.OpenFormatStream(context.Background(), "jNQXAC9IVRw", 18)
	if err != nil {
		t.Fatalf("OpenFormatStream() error = %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("first Close() = %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
}
