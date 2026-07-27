package downloader

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestHLSDownloader_VODConcurrentOrdered verifies that a completed (VOD)
// playlist is downloaded with the concurrent path (MaxConcurrency > 1) yet the
// output is written in strict playlist order.
func TestHLSDownloader_VODConcurrentOrdered(t *testing.T) {
	const numSegments = 40

	var concurrent int32
	var maxConcurrent int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n")
			for i := 0; i < numSegments; i++ {
				fmt.Fprintf(w, "#EXTINF:2.0,\nsegment-%d.ts\n", i)
			}
			fmt.Fprint(w, "#EXT-X-ENDLIST\n")
			return
		}
		// Segment: track overlap to prove parallelism happened.
		cur := atomic.AddInt32(&concurrent, 1)
		for {
			old := atomic.LoadInt32(&maxConcurrent)
			if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, cur) {
				break
			}
		}
		// Brief delay so concurrent fetches overlap observably (instant
		// responses would complete before the next goroutine is scheduled).
		time.Sleep(20 * time.Millisecond)
		defer atomic.AddInt32(&concurrent, -1)

		var idx int
		fmt.Sscanf(r.URL.Path, "/segment-%d.ts", &idx)
		fmt.Fprintf(w, "seg%d;", idx)
	}))
	defer server.Close()

	dl := NewHLSDownloader(server.Client(), server.URL+"/playlist.m3u8").
		WithTransportConfig(TransportConfig{MaxConcurrency: 8})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("download failed: %v", err)
	}

	var want bytes.Buffer
	for i := 0; i < numSegments; i++ {
		fmt.Fprintf(&want, "seg%d;", i)
	}
	if buf.String() != want.String() {
		t.Fatalf("output not in playlist order.\n got: %q\nwant: %q", buf.String(), want.String())
	}
	if got := atomic.LoadInt32(&maxConcurrent); got < 2 {
		t.Fatalf("expected concurrent segment fetches, observed max concurrency %d", got)
	}
}

// TestHLSDownloader_ConcurrentReportsRootCauseError verifies that when a later
// segment genuinely fails, the error surfaced names that segment (and its real
// cause) rather than the context.Canceled it induces on an earlier in-flight
// sibling that the in-order writer reaches first.
func TestHLSDownloader_ConcurrentReportsRootCauseError(t *testing.T) {
	const numSegments = 8
	const failIdx = 5

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n")
			for i := 0; i < numSegments; i++ {
				fmt.Fprintf(w, "#EXTINF:2.0,\nsegment-%d.ts\n", i)
			}
			fmt.Fprint(w, "#EXT-X-ENDLIST\n")
			return
		}
		var idx int
		fmt.Sscanf(r.URL.Path, "/segment-%d.ts", &idx)
		if idx == failIdx {
			// Real root cause: fail fast so the cancel races the slow siblings.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// Earlier segments are slow, so they are still in-flight (and get
		// canceled) when failIdx fails.
		time.Sleep(300 * time.Millisecond)
		fmt.Fprintf(w, "seg%d;", idx)
	}))
	defer server.Close()

	dl := NewHLSDownloader(server.Client(), server.URL+"/playlist.m3u8").
		WithTransportConfig(TransportConfig{MaxConcurrency: 8})

	err := dl.Download(context.Background(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected download error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "seq=5") {
		t.Fatalf("error should name the failing segment seq=5, got: %v", msg)
	}
	if strings.Contains(msg, "context canceled") {
		t.Fatalf("error should report the root cause, not an induced cancellation: %v", msg)
	}
}

// TestHLSDownloader_VODSingleConcurrencySequential verifies the concurrent path
// is not engaged when MaxConcurrency <= 1 (output still correct).
func TestHLSDownloader_VODSingleConcurrencySequential(t *testing.T) {
	const numSegments = 5
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n")
			for i := 0; i < numSegments; i++ {
				fmt.Fprintf(w, "#EXTINF:2.0,\nsegment-%d.ts\n", i)
			}
			fmt.Fprint(w, "#EXT-X-ENDLIST\n")
			return
		}
		var idx int
		fmt.Sscanf(r.URL.Path, "/segment-%d.ts", &idx)
		fmt.Fprintf(w, "seg%d;", idx)
	}))
	defer server.Close()

	dl := NewHLSDownloader(server.Client(), server.URL+"/playlist.m3u8").
		WithTransportConfig(TransportConfig{MaxConcurrency: 1})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("download failed: %v", err)
	}
	want := "seg0;seg1;seg2;seg3;seg4;"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}
