package client

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSrv3CaptionTextAndTiming(t *testing.T) {
	out, err := parseTranscriptXML([]byte(`<timedtext format="3"><body><p t="1234" d="2000"><s>Hello &amp; </s><s>world</s></p></body></timedtext>`))
	if err != nil || len(out) != 1 || out[0].StartSec != 1.234 || out[0].DurSec != 2 || out[0].Text != "Hello & world" {
		t.Fatalf("%+v %v", out, err)
	}
}
func TestEmptyReverseRangeAndStepOverflow(t *testing.T) {
	items := []PlaylistItem{{VideoID: "a"}, {VideoID: "b"}}
	for _, expr := range []string{"-10:-20:-1", "10:8:-1"} {
		out, err := SelectPlaylistItems(items, expr)
		if err != nil || len(out) != 0 {
			t.Fatalf("%s: %+v %v", expr, out, err)
		}
	}
	out, err := SelectPlaylistItems(items, fmt.Sprintf("1:2:%d", math.MaxInt))
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
}
func TestTemplateDoesNotExpandInsertedValues(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := RenderOutputTemplate("%(title)s.%(ext)s", OutputTemplateData{Title: "%(ext)s", Ext: "mp4"})
		if got != "%(ext)s.mp4" {
			t.Fatal(got)
		}
	}
}
func TestSourceIdentityJSONAndArchive(t *testing.T) {
	info := &VideoInfo{ID: "123", SourceName: "soop", WebpageURL: "https://vod.sooplive.co.kr/player/123"}
	payload := BuildYTDLPDumpSingleJSON(info.WebpageURL, info)
	if payload.Extractor != "soop" || payload.WebpageURL != info.WebpageURL {
		t.Fatal(payload)
	}
	path := filepath.Join(t.TempDir(), "archive")
	a, err := OpenDownloadArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Add(ArchiveID(info)); err != nil {
		t.Fatal(err)
	}
	a.Close()
	a, err = OpenDownloadArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if !a.Has("soop 123") {
		t.Fatal("missing source archive entry")
	}
}
func TestThumbnailFailurePreservesExistingFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" {
			t.Error("missing custom header")
		}
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, "short")
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "thumb")
	os.WriteFile(path, []byte("original"), 0600)
	c := New(Config{HTTPClient: srv.Client(), RequestHeaders: http.Header{"X-Test": []string{"yes"}}})
	defer c.Close()
	if err := c.DownloadThumbnail(context.Background(), &VideoInfo{ThumbnailURL: srv.URL}, path); err == nil {
		t.Fatal("accepted truncated response")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "original" {
		t.Fatal("previous thumbnail overwritten")
	}
}
