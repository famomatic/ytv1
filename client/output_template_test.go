package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderOutputPathTemplate_ReplacesAndSanitizes(t *testing.T) {
	got := renderOutputPathTemplate(
		"%(title)s-%(id)s-%(itag)s.%(ext)s",
		outputTemplateData{
			VideoID:  "jNQXAC9IVRw",
			Title:    `A:/B*Title`,
			Uploader: "jawed",
			Ext:      "mp4",
			Itag:     "18",
		},
	)
	if got != "A__B_Title-jNQXAC9IVRw-18.mp4" {
		t.Fatalf("rendered path = %q", got)
	}
}

func TestDownload_UsesOutputTemplateTokens(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	mediaBase := "https://media.example"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"A:/B*Title","author":"jawed"},
					"streamingData":{"formats":[
						{"itag":18,"url":"` + mediaBase + `/v.mp4","mimeType":"video/mp4","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/s/player/"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`var cfg={signatureTimestamp:20494};`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/v.mp4":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("payload")), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
	})

	template := filepath.Join(t.TempDir(), "%(title)s-%(id)s-%(itag)s.%(ext)s")
	res, err := c.Download(context.Background(), videoID, DownloadOptions{
		Itag:       18,
		OutputPath: template,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	wantSuffix := "A__B_Title-jNQXAC9IVRw-18.mp4"
	if filepath.Base(res.OutputPath) != wantSuffix {
		t.Fatalf("output file = %q, want suffix %q", filepath.Base(res.OutputPath), wantSuffix)
	}
}
