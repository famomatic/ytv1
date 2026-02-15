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
