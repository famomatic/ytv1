package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadClientFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestRegression_DSYFmhjDbvs_DownloadMergeLifecycle(t *testing.T) {
	mediaBase := "https://media.example"
	fixture := loadClientFixture(t, "dsyfmhjdbvs_player_response.json")
	var extractionEvents []ExtractionEvent
	var downloadEvents []DownloadEvent

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				payload := strings.ReplaceAll(fixture, "__BASE_URL__", mediaBase)
				payload = strings.ReplaceAll(payload, "__BASE_URL_ESCAPED__", strings.ReplaceAll(mediaBase, ":", "%3A"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(payload)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<html><script src="/s/player/test/player_ias.vflset/ko_KR/base.js"></script></html>`)),
				}, nil
			case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/base.js"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(testPlayerJS())),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/manifest.mpd?n=abcd":
				manifest := `<?xml version="1.0" encoding="UTF-8"?><MPD><Period><AdaptationSet mimeType="audio/webm"><Representation id="251" bandwidth="128000"><BaseURL>` + mediaBase + `/videoplayback?itag=251&n=abcd</BaseURL></Representation></AdaptationSet></Period></MPD>`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(manifest)),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/manifest.m3u8?n=abcd":
				body := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=700000,CODECS=\"vp9,opus\"\n" + mediaBase + "/videoplayback?itag=248&n=abcd\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(body)),
				}, nil
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.String(), mediaBase+"/videoplayback?"):
				itag := r.URL.Query().Get("itag")
				if itag == "248" {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(bytes.NewBufferString("video")),
					}, nil
				}
				if itag == "251" {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(bytes.NewBufferString("audio")),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString("not found")),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString("not found")),
				}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
		OnExtractionEvent: func(evt ExtractionEvent) {
			extractionEvents = append(extractionEvents, evt)
		},
		OnDownloadEvent: func(evt DownloadEvent) {
			downloadEvents = append(downloadEvents, evt)
		},
	})

	out := filepath.Join(t.TempDir(), "dsyf-merged.webm")
	res, err := c.Download(context.Background(), "https://youtu.be/DSYFmhjDbvs", DownloadOptions{
		Mode:       SelectionModeBest,
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res.OutputPath != out {
		t.Fatalf("result output path=%q want=%q", res.OutputPath, out)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); got != "videoaudio" {
		t.Fatalf("merged payload=%q want=%q", got, "videoaudio")
	}

	var hasChallenge, hasManifest, hasMergeComplete, hasCleanupVideoDelete, hasCleanupAudioDelete bool
	for _, evt := range extractionEvents {
		if evt.Stage == "challenge" && (evt.Phase == "success" || evt.Phase == "partial") {
			hasChallenge = true
		}
		if evt.Stage == "manifest" && evt.Phase == "start" {
			hasManifest = true
		}
	}
	for _, evt := range downloadEvents {
		if evt.Stage == "merge" && evt.Phase == "complete" {
			hasMergeComplete = true
		}
		if evt.Stage == "cleanup" && evt.Phase == "delete" && strings.Contains(evt.Path, ".f248.video") {
			hasCleanupVideoDelete = true
		}
		if evt.Stage == "cleanup" && evt.Phase == "delete" && strings.Contains(evt.Path, ".f251.audio") {
			hasCleanupAudioDelete = true
		}
	}
	if !hasChallenge {
		t.Fatalf("expected challenge event, got extraction=%v", extractionEvents)
	}
	if !hasManifest {
		t.Fatalf("expected manifest event, got extraction=%v", extractionEvents)
	}
	if !hasMergeComplete || !hasCleanupVideoDelete || !hasCleanupAudioDelete {
		t.Fatalf("unexpected download events: %v", downloadEvents)
	}
}
