package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadThumbnail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/thumb.jpg" {
			t.Fatalf("path=%q, want /thumb.jpg", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("jpeg-data"))
	}))
	defer server.Close()

	c := New(Config{HTTPClient: server.Client()})
	out := filepath.Join(t.TempDir(), "nested", "thumb.jpg")
	err := c.DownloadThumbnail(context.Background(), &VideoInfo{
		ID:           "jNQXAC9IVRw",
		ThumbnailURL: server.URL + "/thumb.jpg?width=1280",
	}, out)
	if err != nil {
		t.Fatalf("DownloadThumbnail() error = %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "jpeg-data" {
		t.Fatalf("thumbnail content=%q, want jpeg-data", got)
	}
}

func TestDownloadThumbnailUnavailable(t *testing.T) {
	err := New(Config{}).DownloadThumbnail(context.Background(), &VideoInfo{ID: "missing"}, filepath.Join(t.TempDir(), "thumb.jpg"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error=%v, want ErrUnavailable", err)
	}
}

func TestDownloadThumbnailHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	err := New(Config{HTTPClient: server.Client()}).DownloadThumbnail(context.Background(), &VideoInfo{
		ID:           "jNQXAC9IVRw",
		ThumbnailURL: server.URL + "/missing.jpg",
	}, filepath.Join(t.TempDir(), "thumb.jpg"))
	if err == nil || !strings.Contains(err.Error(), "http status 404") {
		t.Fatalf("error=%v, want http status 404", err)
	}
}

// TestDownloadThumbnail_DropsPartialFileOnWriteError ensures that when the
// thumbnail stream errors mid-copy, no partial (truncated) file is left at
// the output path for the caller to mistake for a complete download.
func TestDownloadThumbnail_DropsPartialFileOnWriteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial-"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		if conn != nil {
			conn.Close()
		}
	}))
	defer server.Close()

	c := New(Config{HTTPClient: server.Client()})
	out := filepath.Join(t.TempDir(), "thumb.jpg")
	_ = c.DownloadThumbnail(context.Background(), &VideoInfo{
		ID:           "jNQXAC9IVRw",
		ThumbnailURL: server.URL + "/thumb.jpg",
	}, out)
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("partial thumbnail file should have been removed, but exists at %s", out)
	}
}
