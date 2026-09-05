package client

import (
	"context"
	"fmt"
	"github.com/famomatic/ytv1/internal/source"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSourceDirectStreamSelectionAndCoalescing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.URL.Path) }))
	defer srv.Close()
	var calls atomic.Int32
	src := &stubSource{name: "stub", prefix: "stub:", extract: func(string) (*source.Media, error) {
		calls.Add(1)
		return &source.Media{ID: "42", Formats: []FormatInfo{{Itag: 1, URL: srv.URL + "/one", HasAudio: true, HasVideo: true}, {Itag: 2, URL: srv.URL + "/two", HasAudio: true, HasVideo: true}}}, nil
	}}
	c := New(Config{HTTPClient: srv.Client()})
	c.sources = []source.Source{src}
	defer c.Close()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.GetVideo(context.Background(), "stub:42"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("extracted %d times", calls.Load())
	}
	body, f, err := c.OpenStream(context.Background(), "stub:42", StreamOptions{Itag: 2})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(data) != "/two" || f.Itag != 2 {
		t.Fatalf("%q %+v %v", data, f, err)
	}
}

func TestMP3HLSUsesMediaAndPartFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://stub.test/" {
			t.Error("missing source header")
		}
		if r.URL.Path == "/a.m3u8" {
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\na.bin\n#EXT-X-ENDLIST\n")
		} else {
			fmt.Fprint(w, "audio")
		}
	}))
	defer srv.Close()
	c := New(Config{HTTPClient: srv.Client(), MP3Transcoder: &mp3TranscoderStub{}})
	defer c.Close()
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "stub:", media: &source.Media{ID: "42", Formats: []FormatInfo{{Itag: 1, URL: srv.URL + "/a.m3u8", Protocol: "hls", HasAudio: true}}, MediaHeaders: http.Header{"Referer": []string{"https://stub.test/"}}}}}
	path := filepath.Join(t.TempDir(), "a.mp3")
	if _, err := c.Download(context.Background(), "stub:42", DownloadOptions{Mode: SelectionModeMP3, OutputPath: path, UsePartFiles: true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "mp3:audio" {
		t.Fatalf("transcoded manifest: %q", b)
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Fatal("part file retained after success")
	}
}
