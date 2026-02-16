package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestGetVideo_ExpandsManifestFormats(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"Me at the zoo","author":"jawed"},
					"streamingData":{
						"formats":[{"itag":18,"url":"https://example.com/v.mp4","mimeType":"video/mp4","bitrate":1000}],
						"dashManifestUrl":"https://example.com/dash.mpd",
						"hlsManifestUrl":"https://example.com/master.m3u8"
					}
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				html := `<html><script>var ytcfg = {"INNERTUBE_API_KEY":"dynamic_key_123"};</script><script src="/s/player/1798f86c/player_es6.vflset/ko_KR/base.js"></script></html>`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(html)),
				}, nil
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/s/player/"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`var cfg={signatureTimestamp:20494};`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == "https://example.com/dash.mpd":
				dash := `<?xml version="1.0" encoding="UTF-8"?>
<MPD><Period><AdaptationSet mimeType="audio/mp4" codecs="mp4a.40.2"><Representation id="140" bandwidth="128000"><BaseURL>https://cdn.example.com/a140.m4a</BaseURL></Representation></AdaptationSet></Period></MPD>`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(dash)),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == "https://example.com/master.m3u8":
				hls := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,CODECS="avc1.4d401f,mp4a.40.2"
https://cdn.example.com/v/itag/22/prog.m3u8
`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(hls)),
				}, nil
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				return nil, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
	})

	info, err := c.GetVideo(context.Background(), "jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("GetVideo() error = %v", err)
	}

	// 1 direct format + DASH + HLS
	if len(info.Formats) < 3 {
		t.Fatalf("expected manifest-expanded formats, got len=%d", len(info.Formats))
	}
	var hasDash, hasHLS bool
	for _, f := range info.Formats {
		if f.Protocol == "dash" {
			hasDash = true
		}
		if f.Protocol == "hls" {
			hasHLS = true
		}
	}
	if !hasDash || !hasHLS {
		t.Fatalf("expected both dash and hls formats, got: %+v", info.Formats)
	}
}

