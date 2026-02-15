package client

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
