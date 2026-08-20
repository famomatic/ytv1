package downloader

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveAdaptiveDownloaderFetchesFragmentsInOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sq") == "" {
			w.Header().Set("X-Head-Seqnum", "4")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Write([]byte("seg" + r.URL.Query().Get("sq")))
	}))
	defer srv.Close()

	var out bytes.Buffer
	dl := NewLiveAdaptiveDownloader(srv.Client(), srv.URL)
	if err := dl.Download(context.Background(), &out); err != nil {
		t.Fatalf("download failed: %v", err)
	}
	// X-Head-Seqnum=4 -> fragments sq=0..2 (last two sequences excluded).
	if out.String() != "seg0seg1seg2" {
		t.Fatalf("fragment order/content = %q, want %q", out.String(), "seg0seg1seg2")
	}
}

func TestLiveAdaptiveDownloaderRejectsMissingSequenceHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dl := NewLiveAdaptiveDownloader(srv.Client(), srv.URL).
		WithTransportConfig(TransportConfig{MaxRetries: 0})
	var out bytes.Buffer
	if err := dl.Download(context.Background(), &out); err == nil {
		t.Fatalf("expected error when X-Head-Seqnum is missing")
	}
}
