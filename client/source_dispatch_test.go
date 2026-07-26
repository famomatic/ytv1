package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/famomatic/ytv1/internal/source"
	"github.com/famomatic/ytv1/internal/types"
)

// stubSource is an in-test Source used to verify client dispatch and the
// media-header plumbing without hitting a real site.
type stubSource struct {
	name    string
	prefix  string
	media   *source.Media
	extract func(input string) (*source.Media, error)
}

func (s *stubSource) Name() string           { return s.name }
func (s *stubSource) Matches(in string) bool { return strings.HasPrefix(in, s.prefix) }
func (s *stubSource) Extract(_ context.Context, in string) (*source.Media, error) {
	if s.extract != nil {
		return s.extract(in)
	}
	return s.media, nil
}

const stubReferer = "https://stub.referer/page"

// newStubSourceServer serves two HLS media playlists (1080p, 720p) and their
// segments, recording whether the Referer header from the source's MediaHeaders
// reached segment requests.
func newStubSourceServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var refererSeen int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/1080.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:2.0,\na1.ts\n#EXTINF:2.0,\na2.ts\n#EXT-X-ENDLIST\n")
		case "/720.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:2.0,\nb1.ts\n#EXT-X-ENDLIST\n")
		case "/a1.ts", "/a2.ts", "/b1.ts":
			if r.Header.Get("Referer") == stubReferer {
				atomic.StoreInt32(&refererSeen, 1)
			}
			seg := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".ts")
			fmt.Fprintf(w, "<%s>", seg)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &refererSeen
}

func stubMedia(srv *httptest.Server) *source.Media {
	headers := http.Header{}
	headers.Set("Referer", stubReferer)
	headers.Set("Origin", "https://stub.origin")
	return &source.Media{
		ID:     "42",
		Title:  "Stub Title",
		Author: "Stub Author",
		Formats: []types.FormatInfo{
			{Itag: 1080, URL: srv.URL + "/1080.m3u8", Protocol: "hls", HasAudio: true, HasVideo: true, Height: 1080, Bitrate: 5000000, SourceClient: "stub", QualityLabel: "1080p"},
			{Itag: 720, URL: srv.URL + "/720.m3u8", Protocol: "hls", HasAudio: true, HasVideo: true, Height: 720, Bitrate: 1500000, SourceClient: "stub", QualityLabel: "720p"},
		},
		MediaHeaders: headers,
	}
}

func TestGetVideoViaSource(t *testing.T) {
	srv, _ := newStubSourceServer(t)
	c := New(Config{HTTPClient: srv.Client()})
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "https://stub.test/", media: stubMedia(srv)}}

	info, err := c.GetVideo(context.Background(), "https://stub.test/v/42")
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if info.ID != "42" || info.Title != "Stub Title" {
		t.Errorf("metadata mismatch: %+v", info)
	}
	if len(info.Formats) != 2 {
		t.Fatalf("got %d formats, want 2", len(info.Formats))
	}
}

func TestDownloadViaSourcePicksBestAndSendsHeaders(t *testing.T) {
	srv, refererSeen := newStubSourceServer(t)
	c := New(Config{HTTPClient: srv.Client()})
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "https://stub.test/", media: stubMedia(srv)}}

	out := filepath.Join(t.TempDir(), "out.ts")
	res, err := c.Download(context.Background(), "https://stub.test/v/42", DownloadOptions{OutputPath: out})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	data, err := os.ReadFile(res.OutputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	// Best (1080p) variant selected -> its two segments, in order.
	if got := string(data); got != "<a1><a2>" {
		t.Fatalf("output = %q, want %q (best 1080p variant)", got, "<a1><a2>")
	}
	if atomic.LoadInt32(refererSeen) != 1 {
		t.Error("source MediaHeaders Referer was not sent on segment requests")
	}
}

func TestDownloadViaSourceEmitsProgress(t *testing.T) {
	srv, _ := newStubSourceServer(t)
	var events int32
	var lastDownloaded int64
	c := New(Config{
		HTTPClient: srv.Client(),
		OnDownloadProgress: func(e DownloadProgressEvent) {
			atomic.AddInt32(&events, 1)
			atomic.StoreInt64(&lastDownloaded, e.Downloaded)
		},
	})
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "https://stub.test/", media: stubMedia(srv)}}

	out := filepath.Join(t.TempDir(), "out.ts")
	if _, err := c.Download(context.Background(), "https://stub.test/v/42", DownloadOptions{OutputPath: out}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if atomic.LoadInt32(&events) == 0 {
		t.Fatal("no download progress events emitted for HLS download (CLI would appear frozen)")
	}
	// Best variant "<a1><a2>" = 8 bytes; final reported total must match.
	if got := atomic.LoadInt64(&lastDownloaded); got != int64(len("<a1><a2>")) {
		t.Errorf("final Downloaded = %d, want %d", got, len("<a1><a2>"))
	}
}

func TestDownloadViaSourceStdoutStreamsMediaWithHeaders(t *testing.T) {
	srv, refererSeen := newStubSourceServer(t)
	c := New(Config{HTTPClient: srv.Client()})
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "https://stub.test/", media: stubMedia(srv)}}

	// `-o -` writes media to os.Stdout; capture it via a pipe.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	captured := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		captured <- string(b)
	}()

	_, dlErr := c.Download(context.Background(), "https://stub.test/v/42", DownloadOptions{OutputPath: "-"})
	w.Close()
	os.Stdout = orig
	got := <-captured

	if dlErr != nil {
		t.Fatalf("Download -o -: %v", dlErr)
	}
	// The HLS format must be run through the segment downloader: stdout carries
	// the concatenated best-variant media, NOT the .m3u8 playlist text.
	if got != "<a1><a2>" {
		t.Fatalf("stdout = %q, want %q (concatenated media, not playlist text)", got, "<a1><a2>")
	}
	// And the source's media headers must reach segment requests (not the
	// default YouTube headers, which the CDN would 403).
	if atomic.LoadInt32(refererSeen) != 1 {
		t.Error("source MediaHeaders Referer was not sent on -o - segment requests")
	}
}

func TestExternalSourceDoesNotShadowYouTube(t *testing.T) {
	// A source that only matches its prefix must not intercept YouTube inputs.
	c := New(Config{})
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "https://stub.test/", media: &source.Media{}}}
	if src := c.matchSource("https://www.youtube.com/watch?v=abcdefghijk"); src != nil {
		t.Errorf("YouTube input matched external source %q", src.Name())
	}
	if src := c.matchSource("https://stub.test/v/1"); src == nil {
		t.Error("stub input did not match stub source")
	}
}
