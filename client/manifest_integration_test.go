package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/famomatic/ytv1/internal/innertube"
)

func TestFetchDASHManifest_UsesRewrittenNURL(t *testing.T) {
	var gotN string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotN = r.URL.Query().Get("n")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("dash-manifest"))
	}))
	defer srv.Close()

	videoID := "jNQXAC9IVRw"
	c := testClientWithSession(
		videoID,
		innertube.Format{Itag: 140, URL: "https://example.com/audio"},
		testPlayerJS(),
	)
	c.config.HTTPClient = srv.Client()
	c.sessions[videoID] = videoSession{
		Response: &innertube.PlayerResponse{
			VideoDetails: innertube.VideoDetails{VideoID: videoID},
			StreamingData: innertube.StreamingData{
				DashManifestURL: srv.URL + "/dash?n=abcd",
			},
		},
		PlayerURL: "/s/player/test/base.js",
	}

	body, err := c.FetchDASHManifest(context.Background(), videoID)
	if err != nil {
		t.Fatalf("FetchDASHManifest() error = %v", err)
	}
	if body != "dash-manifest" {
		t.Fatalf("manifest body = %q, want %q", body, "dash-manifest")
	}
	if gotN != "bcd" {
		t.Fatalf("dash n = %q, want %q", gotN, "bcd")
	}
}

func TestFetchHLSManifest_UsesRewrittenNURL(t *testing.T) {
	var gotN string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotN = r.URL.Query().Get("n")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hls-manifest"))
	}))
	defer srv.Close()

	videoID := "jNQXAC9IVRw"
	c := testClientWithSession(
		videoID,
		innertube.Format{Itag: 140, URL: "https://example.com/audio"},
		testPlayerJS(),
	)
	c.config.HTTPClient = srv.Client()
	c.sessions[videoID] = videoSession{
		Response: &innertube.PlayerResponse{
			VideoDetails: innertube.VideoDetails{VideoID: videoID},
			StreamingData: innertube.StreamingData{
				HlsManifestURL: srv.URL + "/hls?n=abcd",
			},
		},
		PlayerURL: "/s/player/test/base.js",
	}

	body, err := c.FetchHLSManifest(context.Background(), videoID)
	if err != nil {
		t.Fatalf("FetchHLSManifest() error = %v", err)
	}
	if body != "hls-manifest" {
		t.Fatalf("manifest body = %q, want %q", body, "hls-manifest")
	}
	if gotN != "bcd" {
		t.Fatalf("hls n = %q, want %q", gotN, "bcd")
	}
}

func TestFetchManifestMissingURL(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	c := testClientWithSession(
		videoID,
		innertube.Format{Itag: 140, URL: "https://example.com/audio"},
		testPlayerJS(),
	)
	c.sessions[videoID] = videoSession{
		Response: &innertube.PlayerResponse{
			VideoDetails:  innertube.VideoDetails{VideoID: videoID},
			StreamingData: innertube.StreamingData{},
		},
		PlayerURL: "/s/player/test/base.js",
	}

	if _, err := c.FetchDASHManifest(context.Background(), videoID); err == nil {
		t.Fatalf("expected dash error")
	}
	if _, err := c.FetchHLSManifest(context.Background(), videoID); err == nil {
		t.Fatalf("expected hls error")
	}
}

func TestRewriteURLParamPreservesOtherQuery(t *testing.T) {
	in := "https://example.com/m.m3u8?foo=1&n=abcd&bar=2"
	out, err := rewriteURLParam(in, "n", func(s string) (string, error) { return s[1:], nil })
	if err != nil {
		t.Fatalf("rewriteURLParam() error = %v", err)
	}
	u, _ := url.Parse(out)
	if u.Query().Get("foo") != "1" || u.Query().Get("bar") != "2" || u.Query().Get("n") != "bcd" {
		t.Fatalf("unexpected query values: %s", u.RawQuery)
	}
}

