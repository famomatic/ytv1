package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/source"
)

func TestChallengeAndRuntimeCacheBounds(t *testing.T) {
	c := New(Config{SessionCacheMaxEntries: 2, ChallengeCacheMaxEntriesPerPlayer: 3, ChallengeCacheMaxBytesPerPlayer: 32, DeciphererCacheMaxEntries: 2, HTTPClient: &http.Client{Transport: auditRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("var player={};")), Request: r}, nil
	})}})
	defer c.Close()
	for p := 0; p < 5; p++ {
		url := fmt.Sprintf("https://www.youtube.com/s/player/p%d/base.js", p)
		for i := 0; i < 10; i++ {
			c.setChallengeN(url, fmt.Sprint(i), "decoded")
			c.setChallengeSig(url, fmt.Sprint(i), "decoded")
		}
		first, err := c.loadDecipherer(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		second, err := c.loadDecipherer(context.Background(), url)
		if err != nil || first != second {
			t.Fatal("runtime was not reused", err)
		}
	}
	if len(c.challenges) > 2 || len(c.decipherers) > 2 {
		t.Fatal("player cache exceeded limit")
	}
	for _, v := range c.challenges {
		if len(v.n) > 3 || len(v.sig) > 3 {
			t.Fatal("challenge cache exceeded limit")
		}
		var size int
		for _, values := range []map[string]string{v.n, v.sig} {
			for k, v := range values {
				size += len(k) + len(v)
			}
		}
		if size > 32 {
			t.Fatalf("challenge byte budget exceeded: %d", size)
		}
	}
}

func TestRateLimitProtocolOutputMatrix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hls":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\ndata\n#EXT-X-ENDLIST\n")
		case "/dash":
			fmt.Fprint(w, `<MPD><Period><AdaptationSet><Representation id="v"><BaseURL>data</BaseURL></Representation></AdaptationSet></Period></MPD>`)
		default:
			fmt.Fprint(w, strings.Repeat("x", 64))
		}
	}))
	defer srv.Close()
	for _, protocol := range []string{"https", "hls", "dash"} {
		for _, stdout := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stdout=%t", protocol, stdout), func(t *testing.T) {
				c := New(Config{HTTPClient: srv.Client(), DownloadTransport: DownloadTransportConfig{RateLimitBytesPerSecond: 1, MaxConcurrency: 1}})
				defer c.Close()
				path := filepath.Join(t.TempDir(), "out")
				if stdout {
					f, err := os.Create(path)
					if err != nil {
						t.Fatal(err)
					}
					original := os.Stdout
					os.Stdout = f
					defer func() { os.Stdout = original; f.Close() }()
					path = "-"
				}
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				err := c.downloadStream(ctx, "abcdefghijk", srv.URL+"/"+protocol, path, FormatInfo{Protocol: protocol, RepresentationID: "v"}, false, true)
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("rate limit/cancellation lost: %v", err)
				}
			})
		}
	}
}

func TestSourceHeadersSurviveMidDownloadEviction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Source") != "required" {
			http.Error(w, "lost source header", 403)
			return
		}
		fmt.Fprint(w, "media")
	}))
	defer srv.Close()
	var c *Client
	var once sync.Once
	c = New(Config{HTTPClient: srv.Client(), SessionCacheMaxEntries: 1, OnDownloadEvent: func(e DownloadEvent) {
		once.Do(func() { c.putSession("unrelated", videoSession{Info: &VideoInfo{ID: "unrelated"}}) })
	}})
	defer c.Close()
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "stub:", media: &source.Media{ID: "42", MediaHeaders: http.Header{"X-Source": {"required"}}, Formats: []FormatInfo{{Itag: 1, URL: srv.URL, HasAudio: true, HasVideo: true}}}}}
	result, err := c.Download(context.Background(), "stub:42", DownloadOptions{OutputPath: filepath.Join(t.TempDir(), "out"), UsePartFiles: true})
	if err != nil || result.Bytes != 5 {
		t.Fatalf("source operation lost during eviction: %+v %v", result, err)
	}
}
