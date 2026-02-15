package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type mp3TranscoderStub struct {
	called bool
	meta   MP3TranscodeMetadata
}

func (s *mp3TranscoderStub) TranscodeToMP3(ctx context.Context, src io.Reader, dst io.Writer, meta MP3TranscodeMetadata) (int64, error) {
	s.called = true
	s.meta = meta
	body, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	out := []byte("mp3:" + string(body))
	n, err := dst.Write(out)
	return int64(n), err
}

func TestDownload_MP3ModeRequiresTranscoder(t *testing.T) {
	c := newMockClientForPlayerJSON(t, `{
		"playabilityStatus":{"status":"OK"},
		"videoDetails":{"videoId":"jNQXAC9IVRw","title":"Me at the zoo","author":"jawed"},
		"streamingData":{"adaptiveFormats":[{"itag":140,"url":"https://media.local/audio","mimeType":"audio/mp4","bitrate":128000}]}
	}`)

	_, err := c.Download(context.Background(), "jNQXAC9IVRw", DownloadOptions{
		Mode:       SelectionModeMP3,
		OutputPath: filepath.Join(t.TempDir(), "out.mp3"),
	})
	if err == nil {
		t.Fatal("expected error for missing mp3 transcoder")
	}
	if !errors.Is(err, ErrMP3TranscoderNotConfigured) {
		t.Fatalf("expected ErrMP3TranscoderNotConfigured, got %v", err)
	}
	var typedErr *MP3TranscoderError
	if !errors.As(err, &typedErr) {
		t.Fatalf("expected MP3TranscoderError type, got %T", err)
	}
}

func TestDownload_MP3ModeWithTranscoder(t *testing.T) {
	transcoder := &mp3TranscoderStub{}

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
			json := `{
				"playabilityStatus":{"status":"OK"},
				"videoDetails":{"videoId":"jNQXAC9IVRw","title":"Me at the zoo","author":"jawed"},
				"streamingData":{"adaptiveFormats":[{"itag":140,"url":"https://media.local/audio","mimeType":"audio/mp4","bitrate":128000}]}
			}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(json))}, nil
		case r.Method == http.MethodGet && r.URL.Path == "/watch":
			html := `<html><script src="/s/player/1798f86c/player_es6.vflset/ko_KR/base.js"></script></html>`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(html))}, nil
		case r.Method == http.MethodGet && r.URL.Host == "media.local" && r.URL.Path == "/audio":
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString("source-audio"))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		MP3Transcoder:   transcoder,
	})

	outPath := filepath.Join(t.TempDir(), "out.mp3")
	result, err := c.Download(context.Background(), "jNQXAC9IVRw", DownloadOptions{
		Mode:       SelectionModeMP3,
		OutputPath: outPath,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if !transcoder.called {
		t.Fatal("expected transcoder to be called")
	}
	if transcoder.meta.VideoID != "jNQXAC9IVRw" || transcoder.meta.SourceItag != 140 || transcoder.meta.SourceMimeType != "audio/mp4" {
		t.Fatalf("unexpected transcode metadata: %+v", transcoder.meta)
	}
	if result.Itag != 140 {
		t.Fatalf("result itag=%d, want 140", result.Itag)
	}
	if result.OutputPath != outPath {
		t.Fatalf("result output path=%q, want %q", result.OutputPath, outPath)
	}
	if result.Bytes != int64(len("mp3:source-audio")) {
		t.Fatalf("result bytes=%d, want %d", result.Bytes, len("mp3:source-audio"))
	}
}
