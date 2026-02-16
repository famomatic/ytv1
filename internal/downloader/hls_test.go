package downloader

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHLSDownloader_LiveStream(t *testing.T) {
	// Mock Server Logic
	// Serve a playlist that updates every call or time based.

	const targetDuration = 1 // 1 second for fast test

	// Protected state
	var mu sync.Mutex
	seq := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/playlist.m3u8":
			// Generate playlist with sliding window window size 3
			// [seq, seq+1, seq+2]
			// We increment seq every request? No, that's too fast.
			// Let's increment seq every 500ms? Or rely on client polling.
			// Let's just return a static list for now, but update it when 'update' param is present?
			// Simplest: The test runner increments seq.

			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n")
			fmt.Fprintf(w, "#EXT-X-VERSION:3\n")
			fmt.Fprintf(w, "#EXT-X-TARGETDURATION:%d\n", targetDuration)
			fmt.Fprintf(w, "#EXT-X-MEDIA-SEQUENCE:%d\n", seq)

			for i := 0; i < 3; i++ {
				s := seq + i
				fmt.Fprintf(w, "#EXTINF:1.0,\n")
				fmt.Fprintf(w, "segment-%d.ts\n", s)
			}

		default:
			// Segment
			if r.URL.Path == "/segment-0.ts" {
				w.Write([]byte("segment0-data"))
				return
			}
			if r.URL.Path == "/segment-1.ts" {
				w.Write([]byte("segment1-data"))
				return
			}
			if r.URL.Path == "/segment-2.ts" {
				w.Write([]byte("segment2-data"))
				return
			}
			if r.URL.Path == "/segment-3.ts" {
				w.Write([]byte("segment3-data"))
				return
			}
			if r.URL.Path == "/segment-4.ts" {
				w.Write([]byte("segment4-data"))
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := server.Client()
	dl := NewHLSDownloader(client, server.URL+"/playlist.m3u8")

	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())

	// Run downloader in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- dl.Download(ctx, &buf)
	}()

	// Simulate live stream progression
	// T=0: Playlist has 0, 1, 2. Downloader gets 0, 1, 2.
	time.Sleep(500 * time.Millisecond) // Let it fetch initial

	mu.Lock()
	seq = 1 // Playlist has 1, 2, 3. Downloader sees 3 is new. (1, 2 dup)
	mu.Unlock()

	time.Sleep(1200 * time.Millisecond) // Wait for refresh (TargetDuration=1s)

	mu.Lock()
	seq = 2 // Playlist has 2, 3, 4. Downloader sees 4 is new.
	mu.Unlock()

	time.Sleep(1200 * time.Millisecond)

	cancel() // Stop downloader

	err := <-errCh
	if err != nil && err != context.Canceled {
		t.Fatalf("Download failed: %v", err)
	}

	output := buf.String()
	// Expected: segment0-datasegment1-datasegment2-datasegment3-datasegment4-data
	// Note: 0,1,2 might be fetched. Then 3. Then 4.
	// Since standard HLS client starts from end of live window usually?
	// Ah, live start behavior.
	// yt-dlp `--live-from-start` tries to go back.
	// Standard players start from close to live edge (last 3 segments).
	// My implementation iterates all segments in manifest.
	// Initial manifest: 0, 1, 2. My code downloads all 0, 1, 2.
	// Next manifest: 1, 2, 3. My code sees 3 (seq > lastSeq 2). Downloads 3.
	// Next manifest: 2, 3, 4. My code sees 4 (seq > lastSeq 3). Downloads 4.

	expected := "segment0-datasegment1-datasegment2-datasegment3-datasegment4-data"
	if output != expected {
		// It might be possible that timing caused missed segments if refresh was slow?
		// Or if logic is wrong.
		// Let's check what we got.
		t.Errorf("Output mismatch.\nGot: %q\nWant: %q", output, expected)
	}
}

func TestHLSDownloader_PropagatesRequestHeaders(t *testing.T) {
	const headerName = "X-Test-Header"
	const headerValue = "ytv1-hls"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(headerName); got != headerValue {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/playlist.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1.0,\nsegment-1.ts\n#EXT-X-ENDLIST\n")
		case "/segment-1.ts":
			w.Write([]byte("seg-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dl := NewHLSDownloader(server.Client(), server.URL+"/playlist.m3u8").WithRequestHeaders(http.Header{
		headerName: []string{headerValue},
	})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := buf.String(); got != "seg-data" {
		t.Fatalf("segment payload mismatch: got=%q", got)
	}
}

func TestHLSDownloader_RetriesManifestFetch(t *testing.T) {
	var manifestCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			if atomic.AddInt32(&manifestCalls, 1) == 1 {
				http.Error(w, "temporary", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1.0,\nsegment-1.ts\n#EXT-X-ENDLIST\n")
		case "/segment-1.ts":
			w.Write([]byte("seg-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dl := NewHLSDownloader(server.Client(), server.URL+"/playlist.m3u8").WithTransportConfig(TransportConfig{
		MaxRetries:       1,
		InitialBackoff:   time.Millisecond,
		MaxBackoff:       time.Millisecond,
		RetryStatusCodes: []int{http.StatusServiceUnavailable},
	})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := buf.String(); got != "seg-data" {
		t.Fatalf("segment payload mismatch: got=%q", got)
	}
	if got := atomic.LoadInt32(&manifestCalls); got != 2 {
		t.Fatalf("manifest call count=%d, want 2", got)
	}
}

func TestHLSDownloader_SkipsUnavailableFragmentsInLive(t *testing.T) {
	var playlistCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			if atomic.AddInt32(&playlistCalls, 1) == 1 {
				fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:0.01\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1.0,\nsegment-0.ts\n#EXTINF:1.0,\nsegment-1.ts\n")
				return
			}
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:0.01\n#EXT-X-MEDIA-SEQUENCE:2\n#EXT-X-ENDLIST\n")
		case "/segment-0.ts":
			http.NotFound(w, r)
		case "/segment-1.ts":
			w.Write([]byte("seg-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dl := NewHLSDownloader(server.Client(), server.URL+"/playlist.m3u8").WithTransportConfig(TransportConfig{
		SkipUnavailableFragments: true,
		MaxSkippedFragments:      2,
	})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := buf.String(); got != "seg-data" {
		t.Fatalf("segment payload mismatch: got=%q", got)
	}
}

func TestHLSDownloader_SkipLimitExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:0.01\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1.0,\nsegment-0.ts\n#EXTINF:1.0,\nsegment-1.ts\n")
		case "/segment-0.ts":
			http.NotFound(w, r)
		case "/segment-1.ts":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	dl := NewHLSDownloader(server.Client(), server.URL+"/playlist.m3u8").WithTransportConfig(TransportConfig{
		SkipUnavailableFragments: true,
		MaxSkippedFragments:      1,
	})
	var buf bytes.Buffer
	if err := dl.Download(ctx, &buf); err == nil {
		t.Fatal("expected skip-limit error")
	}
}
