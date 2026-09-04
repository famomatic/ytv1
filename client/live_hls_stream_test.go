package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	internalmuxer "github.com/famomatic/ytv1/internal/muxer"
)

func TestLiveHLSMergedPlaybackForOpenStreamAndStdout(t *testing.T) {
	originalMux := muxLiveHLSStreams
	defer func() { muxLiveHLSStreams = originalMux }()
	muxLiveHLSStreams = func(ctx context.Context, video, audio internalmuxer.HLSStreamInput, dst io.Writer) error {
		if !strings.Contains(video.URL, "/itag/270/") || !strings.Contains(audio.URL, "/itag/234/") {
			t.Fatalf("mux inputs video=%q audio=%q", video.URL, audio.URL)
		}
		_, err := io.WriteString(dst, "merged-live-av")
		return err
	}

	const playerResponse = `{
		"playabilityStatus":{"status":"OK"},
		"videoDetails":{"videoId":"live1234567","title":"Live","author":"Channel","isLiveContent":true},
		"streamingData":{"hlsManifestUrl":"https://stream.local/master.m3u8"}
	}`
	const master = `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="234",NAME="Default",DEFAULT=YES,AUTOSELECT=YES,URI="https://stream.local/hls/itag/234/playlist/index.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=5420000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1920x1080,FRAME-RATE=30,AUDIO="234"
https://stream.local/hls/itag/270/playlist/index.m3u8
`
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
			body = playerResponse
		case r.Method == http.MethodGet && r.URL.Path == "/watch":
			body = `<html><script src="/s/player/test/base.js"></script></html>`
		case r.Method == http.MethodGet && r.URL.Host == "stream.local" && r.URL.Path == "/master.m3u8":
			body = master
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	c := New(Config{HTTPClient: httpClient, ClientOverrides: []string{"mweb"}})

	rc, format, err := c.OpenStream(context.Background(), "live1234567", StreamOptions{Mode: SelectionModeBest})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	openBytes, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read OpenStream: %v", err)
	}
	if string(openBytes) != "merged-live-av" || !format.HasVideo || !format.HasAudio || format.Protocol != "mpegts" {
		t.Fatalf("OpenStream bytes=%q format=%+v", openBytes, format)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = w
	result, downloadErr := c.Download(context.Background(), "live1234567", DownloadOptions{Mode: SelectionModeBest, OutputPath: "-"})
	_ = w.Close()
	os.Stdout = originalStdout
	var captured bytes.Buffer
	_, readErr := io.Copy(&captured, r)
	_ = r.Close()
	if downloadErr != nil {
		t.Fatalf("Download stdout: %v", downloadErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	if captured.String() != "merged-live-av" {
		t.Fatalf("stdout = %q", captured.String())
	}
	if result == nil || result.Bytes != int64(len("merged-live-av")) || result.Itag != 270 {
		t.Fatalf("Download result = %+v", result)
	}
}
