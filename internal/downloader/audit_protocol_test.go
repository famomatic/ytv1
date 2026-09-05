package downloader

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHLSByteRangeAfterExtinf(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.m3u8" {
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\n#EXT-X-BYTERANGE:2@0\nmedia\n#EXTINF:1,\n#EXT-X-BYTERANGE:2\nmedia\n#EXT-X-ENDLIST\n")
			return
		}
		var start, end int
		fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/4", start, end))
		w.WriteHeader(206)
		fmt.Fprint(w, "ABCD"[start:end+1])
	}))
	defer srv.Close()
	var out bytes.Buffer
	if err := NewHLSDownloader(srv.Client(), srv.URL+"/index.m3u8").Download(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ABCD" {
		t.Fatal(out.String())
	}
}

func TestDASHInheritedNumberTemplatePeriodsAndInit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.mpd" {
			fmt.Fprint(w, `<MPD mediaPresentationDuration="PT5S"><Period duration="PT3S"><BaseURL>p1/</BaseURL><SegmentTemplate duration="2" initialization="init" media="$Number%02d$"/><AdaptationSet><Representation id="v"/></AdaptationSet></Period><Period duration="PT2S"><BaseURL>p2/</BaseURL><AdaptationSet><SegmentTemplate duration="2" initialization="init" media="$Number%02d$"/><Representation id="v"/></AdaptationSet></Period></MPD>`)
			return
		}
		fmt.Fprint(w, r.URL.Path)
	}))
	defer srv.Close()
	var out bytes.Buffer
	if err := NewDASHDownloader(srv.Client(), srv.URL+"/index.mpd", "v").Download(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "/p1/init/p1/01/p1/02/p2/init/p2/01" {
		t.Fatal(out.String())
	}
}
