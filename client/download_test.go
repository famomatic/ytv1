package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/types"
)

func TestDownloadURLToWriter_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	n, err := downloadURLToWriter(context.Background(), srv.Client(), srv.URL, &buf)
	if err != nil {
		t.Fatalf("downloadURLToWriter() error = %v", err)
	}
	if n != int64(len("payload")) {
		t.Fatalf("downloadURLToWriter() bytes = %d, want %d", n, len("payload"))
	}
	if got := buf.String(); got != "payload" {
		t.Fatalf("downloadURLToWriter() body = %q, want %q", got, "payload")
	}
}

func TestDownloadURLToWriter_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if _, err := downloadURLToWriter(context.Background(), srv.Client(), srv.URL, &buf); err == nil {
		t.Fatalf("downloadURLToWriter() error = nil, want non-nil")
	}
}

func TestDownloadURLToWriter_RetryOnTransientStatus(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok-after-retry"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	n, err := downloadURLToWriterWithConfig(context.Background(), srv.Client(), srv.URL, &buf, DownloadTransportConfig{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToWriterWithConfig() error = %v", err)
	}
	if n != int64(len("ok-after-retry")) {
		t.Fatalf("downloadURLToWriterWithConfig() bytes = %d, want %d", n, len("ok-after-retry"))
	}
	if got := buf.String(); got != "ok-after-retry" {
		t.Fatalf("downloadURLToWriterWithConfig() body = %q, want %q", got, "ok-after-retry")
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestDownloadURLToPath_ResumeAppend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=3-" {
			t.Fatalf("range header=%q, want %q", got, "bytes=3-")
		}
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "def")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "resume.bin")
	if err := os.WriteFile(out, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, true, DownloadTransportConfig{
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if n != 6 {
		t.Fatalf("downloadURLToPath() bytes=%d, want 6", n)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); got != "abcdef" {
		t.Fatalf("final content=%q, want %q", got, "abcdef")
	}
}

func TestDownloadURLToPath_ResumeFallbackToFull(t *testing.T) {
	var sawRange bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Range")) != "" {
			sawRange = true
			_, _ = io.WriteString(w, "full-data")
			return
		}
		_, _ = io.WriteString(w, "full-data")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "resume-fallback.bin")
	if err := os.WriteFile(out, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, true, DownloadTransportConfig{
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if !sawRange {
		t.Fatal("expected initial resume range attempt")
	}
	if n != int64(len("full-data")) {
		t.Fatalf("downloadURLToPath() bytes=%d, want %d", n, len("full-data"))
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); got != "full-data" {
		t.Fatalf("final content=%q, want %q", got, "full-data")
	}
}

func TestDownloadURLToPath_Chunked(t *testing.T) {
	payload := []byte(strings.Repeat("chunk-data-", 512))
	var rangeCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		if start < 0 || end < start || end >= len(payload) {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		atomic.AddInt32(&rangeCalls, 1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "chunked.bin")
	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, false, DownloadTransportConfig{
		EnableChunked:  true,
		ChunkSize:      1024,
		MaxConcurrency: 4,
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("downloadURLToPath() bytes=%d, want %d", n, len(payload))
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatal("chunked output mismatch")
	}
	if atomic.LoadInt32(&rangeCalls) <= 1 {
		t.Fatalf("expected multiple range calls, got %d", atomic.LoadInt32(&rangeCalls))
	}
}

func TestDownloadURLToPath_ChunkedCancel(t *testing.T) {
	payload := []byte(strings.Repeat("x", 1024*64))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	out := filepath.Join(t.TempDir(), "chunked-cancel.bin")
	_, err := downloadURLToPath(ctx, srv.Client(), srv.URL, out, false, DownloadTransportConfig{
		EnableChunked:  true,
		ChunkSize:      1024,
		MaxConcurrency: 4,
		MaxRetries:     0,
	})
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
}

func TestDownloadURLToPathWithHeaders_AppliesMediaHeaders(t *testing.T) {
	var gotUA, gotReferer, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotReferer = r.Header.Get("Referer")
		gotOrigin = r.Header.Get("Origin")
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "headers.bin")
	_, err := downloadURLToPathWithHeaders(
		context.Background(),
		srv.Client(),
		srv.URL,
		out,
		false,
		DownloadTransportConfig{},
		"abc123",
		http.Header{"User-Agent": []string{"custom-agent/1.0"}},
	)
	if err != nil {
		t.Fatalf("downloadURLToPathWithHeaders() error = %v", err)
	}
	if gotUA != "custom-agent/1.0" {
		t.Fatalf("User-Agent=%q, want %q", gotUA, "custom-agent/1.0")
	}
	if gotReferer != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("Referer=%q", gotReferer)
	}
	if gotOrigin != "https://www.youtube.com" {
		t.Fatalf("Origin=%q", gotOrigin)
	}
}

type testMuxer struct{}

func (testMuxer) Available() bool { return true }

func (testMuxer) Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error {
	v, err := os.ReadFile(videoPath)
	if err != nil {
		return err
	}
	a, err := os.ReadFile(audioPath)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(v, a...), 0o644)
}

func TestDownloadAndMerge_DefaultCleansIntermediateFiles(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	var events []DownloadEvent
	mediaBase := "https://media.example"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{"adaptiveFormats":[
						{"itag":248,"url":"` + mediaBase + `/v.webm","mimeType":"video/webm","bitrate":1000},
						{"itag":251,"url":"` + mediaBase + `/a.webm","mimeType":"audio/webm","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/v.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("video")), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/a.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("audio")), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
		OnDownloadEvent: func(evt DownloadEvent) { events = append(events, evt) },
	})
	out := filepath.Join(t.TempDir(), "merged.webm")
	res, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:       SelectionModeBest,
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res.OutputPath != out {
		t.Fatalf("output path=%q want=%q", res.OutputPath, out)
	}
	videoPath := out + ".f248.video"
	audioPath := out + ".f251.audio"
	if _, err := os.Stat(videoPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected video intermediate deleted, stat err=%v", err)
	}
	if _, err := os.Stat(audioPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected audio intermediate deleted, stat err=%v", err)
	}
	var hasMergeComplete, hasCleanupDelete bool
	for _, evt := range events {
		if evt.Stage == "merge" && evt.Phase == "complete" {
			hasMergeComplete = true
		}
		if evt.Stage == "cleanup" && evt.Phase == "delete" {
			hasCleanupDelete = true
		}
	}
	if !hasMergeComplete || !hasCleanupDelete {
		t.Fatalf("expected merge complete and cleanup delete events, got=%v", events)
	}
}

func TestDownloadAndMerge_KeepIntermediateFiles(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	var events []DownloadEvent
	mediaBase := "https://media.example"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{"adaptiveFormats":[
						{"itag":248,"url":"` + mediaBase + `/v.webm","mimeType":"video/webm","bitrate":1000},
						{"itag":251,"url":"` + mediaBase + `/a.webm","mimeType":"audio/webm","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/v.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("video")), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/a.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("audio")), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
		OnDownloadEvent: func(evt DownloadEvent) { events = append(events, evt) },
	})
	out := filepath.Join(t.TempDir(), "merged.webm")
	_, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:                  SelectionModeBest,
		OutputPath:            out,
		KeepIntermediateFiles: true,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	videoPath := out + ".f248.video"
	audioPath := out + ".f251.audio"
	if _, err := os.Stat(videoPath); err != nil {
		t.Fatalf("expected video intermediate kept, stat err=%v", err)
	}
	if _, err := os.Stat(audioPath); err != nil {
		t.Fatalf("expected audio intermediate kept, stat err=%v", err)
	}
	var hasCleanupSkip bool
	for _, evt := range events {
		if evt.Stage == "cleanup" && evt.Phase == "skip" {
			hasCleanupSkip = true
		}
	}
	if !hasCleanupSkip {
		t.Fatalf("expected cleanup skip event, got=%v", events)
	}
}

func TestDownloadFailureProvidesAttemptDetails(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	mediaURL := "https://media.example/v.webm?itag=18&pot=token&sig=xyz"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{"formats":[
						{"itag":18,"url":"` + mediaURL + `","mimeType":"video/mp4","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/s/player/test/base.js":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(testPlayerJS())),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.String(), "https://media.example/v.webm?"):
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Body:       io.NopCloser(strings.NewReader("forbidden")),
					Header:     make(http.Header),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
	})

	_, err := c.Download(context.Background(), videoID, DownloadOptions{
		Itag: 18,
	})
	if err == nil {
		t.Fatal("expected download failure error, got nil")
	}

	attempts, ok := AttemptDetails(err)
	if !ok || len(attempts) != 1 {
		t.Fatalf("AttemptDetails() ok=%v attempts=%v err=%v", ok, attempts, err)
	}
	a := attempts[0]
	if a.Stage != "download" || a.HTTPStatus != http.StatusForbidden {
		t.Fatalf("unexpected stage/status: %+v", a)
	}
	if a.Itag != 18 || a.Protocol != "https" {
		t.Fatalf("unexpected itag/protocol: %+v", a)
	}
	if a.URLHost != "media.example" || a.URLHasN || !a.URLHasPOT || !a.URLHasSignature {
		t.Fatalf("unexpected url policy details: %+v", a)
	}
	if a.Client == "" {
		t.Fatalf("expected source client in attempt details, got: %+v", a)
	}
}

func TestDownloadPrefersNonCipheredFallbackSelection(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{
						"adaptiveFormats":[
							{"itag":248,"signatureCipher":"url=https%3A%2F%2Fmedia.example%2Fcipher-video.webm&s=abc&sp=sig","mimeType":"video/webm","bitrate":2000000},
							{"itag":251,"signatureCipher":"url=https%3A%2F%2Fmedia.example%2Fcipher-audio.webm&s=xyz&sp=sig","mimeType":"audio/webm","bitrate":192000},
							{"itag":135,"url":"https://media.example/plain-video.mp4","mimeType":"video/mp4","bitrate":700000},
							{"itag":140,"url":"https://media.example/plain-audio.m4a","mimeType":"audio/mp4","bitrate":128000}
						]
					}
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/player_ias.vflset/en_US/base.js"></script></html>`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/s/player/test/player_ias.vflset/en_US/base.js":
				// Intentionally broken JS: if ciphered selection is attempted, resolve should fail.
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`var broken = true;`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == "https://media.example/plain-video.mp4":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("video")),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == "https://media.example/plain-audio.m4a":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("audio")),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "cipher-video.webm"):
				t.Fatalf("ciphered video should not be selected")
				return nil, nil
			case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "cipher-audio.webm"):
				t.Fatalf("ciphered audio should not be selected")
				return nil, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
	})

	out := filepath.Join(t.TempDir(), "merged.mp4")
	res, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:       SelectionModeBest,
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res.OutputPath != out {
		t.Fatalf("output path=%q want=%q", res.OutputPath, out)
	}
}

func TestDownloadFallsBackToSingleWhenMergeChallengeUnsolved(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{
						"formats":[{"itag":18,"url":"https://media.example/muxed.mp4","mimeType":"video/mp4","bitrate":120000}],
						"adaptiveFormats":[
							{"itag":248,"signatureCipher":"url=https%3A%2F%2Fmedia.example%2Fcipher-video.webm&s=abc&sp=sig","mimeType":"video/webm","bitrate":2000000},
							{"itag":251,"signatureCipher":"url=https%3A%2F%2Fmedia.example%2Fcipher-audio.webm&s=xyz&sp=sig","mimeType":"audio/webm","bitrate":192000}
						]
					}
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/player_ias.vflset/en_US/base.js"></script></html>`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/s/player/test/player_ias.vflset/en_US/base.js":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`var broken = true;`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == "https://media.example/muxed.mp4":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("muxed")),
					Header:     make(http.Header),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
	})
	out := filepath.Join(t.TempDir(), "fallback.mp4")
	res, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:       SelectionModeBest,
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res.Itag != 18 {
		t.Fatalf("expected fallback muxed itag=18, got %d", res.Itag)
	}
}
